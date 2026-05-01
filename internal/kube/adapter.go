package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const fieldManager = "kubeshipper"

// DeployService applies Deployment + (optionally) Service + Ingress for a spec.
// Uses server-side apply with our field manager so subsequent applies own
// only the fields we set, avoiding accidental clobbering.
func (c *Client) DeployService(ctx context.Context, spec *ServiceSpec) error {
	ns, err := c.ResolveNamespace(spec.Namespace)
	if err != nil {
		return err
	}
	if err := c.applyDeployment(ctx, spec, ns); err != nil {
		return fmt.Errorf("apply deployment: %w", err)
	}
	if spec.Port != nil {
		if err := c.applyService(ctx, spec, ns); err != nil {
			return fmt.Errorf("apply service: %w", err)
		}
		if spec.Public {
			if err := c.applyIngress(ctx, spec, ns); err != nil {
				return fmt.Errorf("apply ingress: %w", err)
			}
		}
	}
	return nil
}

func (c *Client) DeleteService(ctx context.Context, id, namespace string) error {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return err
	}
	bg := metav1.DeletePropagationBackground
	opts := metav1.DeleteOptions{PropagationPolicy: &bg}
	_ = c.KC.AppsV1().Deployments(ns).Delete(ctx, id, opts)
	_ = c.KC.CoreV1().Services(ns).Delete(ctx, id, opts)
	_ = c.KC.NetworkingV1().Ingresses(ns).Delete(ctx, id, opts)
	return nil
}

func (c *Client) RestartService(ctx context.Context, id, namespace string) error {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return err
	}
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubeshipper.io/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339Nano),
	))
	_, err = c.KC.AppsV1().Deployments(ns).Patch(ctx, id, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (c *Client) UpdateDeploymentImage(ctx context.Context, id, namespace, container, image string) error {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return err
	}
	patchBody := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubeshipper.io/autoRolloutAt":    time.Now().UTC().Format(time.RFC3339Nano),
						"kubeshipper.io/autoRolloutImage": image,
					},
				},
				"spec": map[string]any{
					"containers": []map[string]string{{
						"name":  container,
						"image": image,
					}},
				},
			},
		},
	}
	patch, err := json.Marshal(patchBody)
	if err != nil {
		return err
	}
	_, err = c.KC.AppsV1().Deployments(ns).Patch(ctx, id, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}

type ServiceLiveStatus struct {
	Ready         bool                         `json:"ready"`
	ReadyReplicas int32                        `json:"readyReplicas"`
	TotalReplicas int32                        `json:"totalReplicas"`
	Conditions    []appsv1.DeploymentCondition `json:"conditions,omitempty"`
	Reason        string                       `json:"reason,omitempty"`
}

func (c *Client) ServiceStatus(ctx context.Context, id, namespace string) (ServiceLiveStatus, error) {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return ServiceLiveStatus{}, err
	}
	d, err := c.KC.AppsV1().Deployments(ns).Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ServiceLiveStatus{Ready: false, Reason: "Deployment not found"}, nil
		}
		return ServiceLiveStatus{}, err
	}
	desired := int32(0)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	ready := d.Status.ReadyReplicas
	return ServiceLiveStatus{
		Ready:         desired == 0 || (ready > 0 && ready == desired),
		ReadyReplicas: ready,
		TotalReplicas: desired,
		Conditions:    d.Status.Conditions,
	}, nil
}

// --- builders ---

func (c *Client) applyDeployment(ctx context.Context, spec *ServiceSpec, ns string) error {
	envs := envFromMap(spec.Env)

	container := corev1.Container{
		Name:  "app",
		Image: spec.Image,
		Env:   envs,
	}
	if spec.Port != nil {
		container.Ports = []corev1.ContainerPort{{ContainerPort: int32(*spec.Port)}}
	}
	if spec.Resources != nil {
		container.Resources = corev1.ResourceRequirements{
			Requests: parseRequests(spec.Resources.Requests),
			Limits:   parseRequests(spec.Resources.Limits),
		}
	}

	pod := corev1.PodSpec{Containers: []corev1.Container{container}}
	if spec.ImagePullSecret != "" {
		pod.ImagePullSecrets = []corev1.LocalObjectReference{{Name: spec.ImagePullSecret}}
	}

	replicas := int32(1)
	if spec.Replicas != nil {
		replicas = int32(*spec.Replicas)
	}

	d := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: ns,
			Labels: map[string]string{"app": spec.Name, "app.kubernetes.io/managed-by": "kubeshipper"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": spec.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": spec.Name}},
				Spec:       pod,
			},
		},
	}

	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = c.KC.AppsV1().Deployments(ns).Patch(ctx, spec.Name,
		types.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: fieldManager, Force: ptrTrue()},
	)
	return err
}

func (c *Client) applyService(ctx context.Context, spec *ServiceSpec, ns string) error {
	port := int32(*spec.Port)
	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: ns,
			Labels: map[string]string{"app": spec.Name, "app.kubernetes.io/managed-by": "kubeshipper"}},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": spec.Name},
			Ports: []corev1.ServicePort{{
				Port: port, TargetPort: intstr.FromInt32(port), Protocol: corev1.ProtocolTCP,
			}},
		},
	}
	body, err := json.Marshal(svc)
	if err != nil {
		return err
	}
	_, err = c.KC.CoreV1().Services(ns).Patch(ctx, spec.Name,
		types.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: fieldManager, Force: ptrTrue()},
	)
	return err
}

func (c *Client) applyIngress(ctx context.Context, spec *ServiceSpec, ns string) error {
	prefix := netv1.PathTypePrefix
	port := int32(*spec.Port)
	rule := netv1.IngressRule{}
	if spec.Hostname != "" {
		rule.Host = spec.Hostname
	}
	rule.IngressRuleValue = netv1.IngressRuleValue{
		HTTP: &netv1.HTTPIngressRuleValue{Paths: []netv1.HTTPIngressPath{{
			Path:     "/",
			PathType: &prefix,
			Backend: netv1.IngressBackend{
				Service: &netv1.IngressServiceBackend{Name: spec.Name, Port: netv1.ServiceBackendPort{Number: port}},
			},
		}}},
	}

	ing := netv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: ns,
			Labels: map[string]string{"app": spec.Name, "app.kubernetes.io/managed-by": "kubeshipper"}},
		Spec: netv1.IngressSpec{Rules: []netv1.IngressRule{rule}},
	}
	body, err := json.Marshal(ing)
	if err != nil {
		return err
	}
	_, err = c.KC.NetworkingV1().Ingresses(ns).Patch(ctx, spec.Name,
		types.ApplyPatchType, body,
		metav1.PatchOptions{FieldManager: fieldManager, Force: ptrTrue()},
	)
	return err
}

func envFromMap(m map[string]string) []corev1.EnvVar {
	if len(m) == 0 {
		return nil
	}
	out := make([]corev1.EnvVar, 0, len(m))
	for k, v := range m {
		out = append(out, corev1.EnvVar{Name: k, Value: v})
	}
	return out
}

func parseRequests(in map[string]string) corev1.ResourceList {
	if len(in) == 0 {
		return nil
	}
	out := corev1.ResourceList{}
	for k, v := range in {
		q, err := resource.ParseQuantity(v)
		if err == nil {
			out[corev1.ResourceName(k)] = q
		}
	}
	return out
}

func ptrTrue() *bool { t := true; return &t }

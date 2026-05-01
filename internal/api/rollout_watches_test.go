package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aerol-ai/kubeshipper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	apiOldDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	apiNewDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type rolloutRegisterResp struct {
	Created bool `json:"created"`
	Watch   struct {
		ID           string `json:"id"`
		Namespace    string `json:"namespace"`
		Deployment   string `json:"deployment"`
		TrackedImage string `json:"tracked_image"`
	} `json:"watch"`
}

type rolloutSyncResp struct {
	Applied bool   `json:"applied"`
	Result  string `json:"result"`
	Watch   struct {
		LatestDigest string `json:"latest_digest"`
	} `json:"watch"`
}

type rolloutTargetsResp struct {
	Namespaces []string `json:"namespaces"`
	Targets    []struct {
		Namespace  string `json:"namespace"`
		Deployment string `json:"deployment"`
		Service    string `json:"service"`
		Containers []struct {
			Name         string `json:"name"`
			Image        string `json:"image"`
			TrackedImage string `json:"tracked_image"`
		} `json:"containers"`
	} `json:"targets"`
}

func TestListRolloutWatchTargets(t *testing.T) {
	srv := newTestServer(t)
	seedDeployment(t, srv, "agent-gateway", []corev1.Container{
		{Name: "app", Image: "ghcr.io/acme/agent:latest"},
		{Name: "sidecar", Image: "busybox:latest"},
	}, true, "")

	rec := do(srv, "GET", "/rollout-watches/targets", nil)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body rolloutTargetsResp
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Namespaces) != 1 || body.Namespaces[0] != "default" {
		t.Fatalf("namespaces: got %#v", body.Namespaces)
	}
	if len(body.Targets) != 1 {
		t.Fatalf("targets length: got %d", len(body.Targets))
	}
	if body.Targets[0].Deployment != "agent-gateway" {
		t.Fatalf("deployment: got %q", body.Targets[0].Deployment)
	}
	if body.Targets[0].Service != "agent-gateway" {
		t.Fatalf("service: got %q", body.Targets[0].Service)
	}
	if len(body.Targets[0].Containers) != 2 {
		t.Fatalf("containers length: got %d", len(body.Targets[0].Containers))
	}
	if body.Targets[0].Containers[0].TrackedImage == "" {
		t.Fatal("expected tracked image for first container")
	}
}

func TestRegisterRolloutWatch_Valid(t *testing.T) {
	srv := newTestServer(t)
	seedDeployment(t, srv, "agent-gateway", []corev1.Container{{Name: "app", Image: "ghcr.io/acme/agent:latest"}}, true, "")

	rec := do(srv, "POST", "/rollout-watches", []byte(`{"namespace":"default","service":"agent-gateway"}`))
	if rec.Code != 201 {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body rolloutRegisterResp
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !body.Created {
		t.Fatal("expected rollout watch to be newly created")
	}
	if body.Watch.Deployment != "agent-gateway" {
		t.Fatalf("deployment: got %q", body.Watch.Deployment)
	}
	if body.Watch.TrackedImage != "ghcr.io/acme/agent:latest" {
		t.Fatalf("tracked image: got %q", body.Watch.TrackedImage)
	}
}

func TestRegisterRolloutWatch_MultiContainerRequiresContainer(t *testing.T) {
	srv := newTestServer(t)
	seedDeployment(t, srv, "agent-gateway", []corev1.Container{
		{Name: "app", Image: "ghcr.io/acme/agent:latest"},
		{Name: "sidecar", Image: "busybox:latest"},
	}, true, "")

	rec := do(srv, "POST", "/rollout-watches", []byte(`{"namespace":"default","deployment":"agent-gateway"}`))
	if rec.Code != 400 {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSyncRolloutWatch_UpdatesDeploymentImageWhenDigestChanges(t *testing.T) {
	srv := newTestServer(t)
	seedDeployment(t, srv, "agent-gateway", []corev1.Container{{Name: "app", Image: "ghcr.io/acme/agent:latest"}}, true, apiOldDigest)
	srv.deps.Kube.ImageResolver = kube.RegistryResolverFunc(func(ctx context.Context, image string, credentials []kube.RegistryCredential) (kube.ResolvedImage, error) {
		return kube.ResolvedImage{Image: image + "@" + apiNewDigest, Digest: apiNewDigest}, nil
	})

	register := do(srv, "POST", "/rollout-watches", []byte(`{"namespace":"default","deployment":"agent-gateway"}`))
	if register.Code != 201 {
		t.Fatalf("register status: got %d: %s", register.Code, register.Body.String())
	}
	var created rolloutRegisterResp
	if err := json.Unmarshal(register.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	syncRec := do(srv, "POST", "/rollout-watches/"+created.Watch.ID+"/sync", nil)
	if syncRec.Code != 200 {
		t.Fatalf("sync status: got %d: %s", syncRec.Code, syncRec.Body.String())
	}
	var syncBody rolloutSyncResp
	if err := json.Unmarshal(syncRec.Body.Bytes(), &syncBody); err != nil {
		t.Fatalf("decode sync response: %v", err)
	}
	if !syncBody.Applied {
		t.Fatal("expected sync to patch the deployment image")
	}
	if syncBody.Result != "updated" {
		t.Fatalf("result: got %q", syncBody.Result)
	}
	if syncBody.Watch.LatestDigest != apiNewDigest {
		t.Fatalf("latest digest: got %q", syncBody.Watch.LatestDigest)
	}

	dep, err := srv.deps.Kube.KC.AppsV1().Deployments("default").Get(context.Background(), "agent-gateway", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if dep.Spec.Template.Spec.Containers[0].Image != "ghcr.io/acme/agent:latest@"+apiNewDigest {
		t.Fatalf("deployment image: got %q", dep.Spec.Template.Spec.Containers[0].Image)
	}
}

func seedDeployment(t *testing.T, srv *Server, name string, containers []corev1.Container, ready bool, digest string) {
	t.Helper()
	one := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec:       corev1.PodSpec{Containers: containers},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	if _, err := srv.deps.Kube.KC.AppsV1().Deployments("default").Create(context.Background(), deployment, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if !ready {
		return
	}
	statuses := []corev1.ContainerStatus{{Name: containers[0].Name}}
	if digest != "" {
		statuses[0].ImageID = "docker-pullable://ghcr.io/acme/agent@" + digest
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.PodSpec{Containers: containers},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
			ContainerStatuses: statuses,
		},
	}
	if _, err := srv.deps.Kube.KC.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
}

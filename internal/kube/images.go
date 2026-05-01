package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type RegistryCredential struct {
	Registry string
	Username string
	Password string
}

type ResolvedImage struct {
	Image  string `json:"image"`
	Digest string `json:"digest"`
}

type RegistryResolver interface {
	Resolve(ctx context.Context, image string, credentials []RegistryCredential) (ResolvedImage, error)
}

type RegistryResolverFunc func(ctx context.Context, image string, credentials []RegistryCredential) (ResolvedImage, error)

func (f RegistryResolverFunc) Resolve(ctx context.Context, image string, credentials []RegistryCredential) (ResolvedImage, error) {
	return f(ctx, image, credentials)
}

type DeploymentImageState struct {
	Namespace       string
	Deployment      string
	Container       string
	TrackedImage    string
	CurrentImage    string
	CurrentDigest   string
	DesiredReplicas int32
	ReadyReplicas   int32
	Stable          bool
	Reason          string
	PullSecrets     []string
}

type defaultRegistryResolver struct{}

type dockerConfigJSON struct {
	Auths map[string]dockerConfigAuth `json:"auths"`
}

type dockerConfigAuth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Auth     string `json:"auth,omitempty"`
}

var digestPattern = regexp.MustCompile(`sha256:[a-f0-9]{64}`)

func (defaultRegistryResolver) Resolve(ctx context.Context, image string, credentials []RegistryCredential) (ResolvedImage, error) {
	ref, err := name.ParseReference(image, name.WithDefaultRegistry("index.docker.io"), name.WithDefaultTag("latest"))
	if err != nil {
		return ResolvedImage{}, fmt.Errorf("parse image reference %q: %w", image, err)
	}

	options := []remote.Option{remote.WithContext(ctx)}
	if auth := credentialForRegistry(ref.Context().RegistryStr(), credentials); auth != nil {
		options = append(options, remote.WithAuth(auth))
	}

	headDesc, err := remote.Head(ref, options...)
	if err == nil {
		return ResolvedImage{Image: pinImageToDigest(image, headDesc.Digest.String()), Digest: headDesc.Digest.String()}, nil
	}
	fullDesc, getErr := remote.Get(ref, options...)
	if getErr != nil {
		return ResolvedImage{}, fmt.Errorf("resolve remote digest for %q: head=%v, get=%w", image, err, getErr)
	}
	digest := fullDesc.Descriptor.Digest.String()
	return ResolvedImage{Image: pinImageToDigest(image, digest), Digest: digest}, nil
}

func (c *Client) InspectDeploymentImage(ctx context.Context, namespace, deployment, container string) (*DeploymentImageState, error) {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return nil, err
	}
	d, err := c.KC.AppsV1().Deployments(ns).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s: %w", ns, deployment, err)
	}
	selected, err := chooseContainer(d.Spec.Template.Spec.Containers, container)
	if err != nil {
		return nil, err
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	stable, reason := deploymentStable(d, desired)
	currentDigest, err := deploymentDigest(ctx, c.KC, ns, d, selected.Name)
	if err != nil {
		return nil, err
	}
	return &DeploymentImageState{
		Namespace:       ns,
		Deployment:      deployment,
		Container:       selected.Name,
		TrackedImage:    trackedImageForWatch(selected.Image),
		CurrentImage:    selected.Image,
		CurrentDigest:   currentDigest,
		DesiredReplicas: desired,
		ReadyReplicas:   d.Status.ReadyReplicas,
		Stable:          stable,
		Reason:          reason,
		PullSecrets:     collectImagePullSecrets(ctx, c.KC, ns, d),
	}, nil
}

func (c *Client) ResolveLatestImage(ctx context.Context, namespace, image string, pullSecretNames []string) (ResolvedImage, error) {
	ns, err := c.ResolveNamespace(namespace)
	if err != nil {
		return ResolvedImage{}, err
	}
	credentials, err := loadRegistryCredentials(ctx, c.KC, ns, pullSecretNames)
	if err != nil {
		return ResolvedImage{}, err
	}
	resolver := c.ImageResolver
	if resolver == nil {
		resolver = defaultRegistryResolver{}
	}
	return resolver.Resolve(ctx, image, credentials)
}

func trackedImageForWatch(image string) string {
	if before, _, ok := strings.Cut(image, "@"); ok {
		if hasExplicitTag(before) {
			return before
		}
		return image
	}
	return image
}

func pinImageToDigest(image, digest string) string {
	if digest == "" {
		return image
	}
	base := image
	hadDigest := false
	if before, _, ok := strings.Cut(image, "@"); ok {
		base = before
		hadDigest = true
	}
	if hadDigest && !hasExplicitTag(base) {
		return base + "@" + digest
	}
	if !hasExplicitTag(base) {
		return base + ":latest@" + digest
	}
	return base + "@" + digest
}

func hasExplicitTag(image string) bool {
	base := image
	if before, _, ok := strings.Cut(image, "@"); ok {
		base = before
	}
	lastSlash := strings.LastIndex(base, "/")
	lastColon := strings.LastIndex(base, ":")
	return lastColon > lastSlash
}

func ExtractDigest(value string) string {
	match := digestPattern.FindString(value)
	return match
}

func chooseContainer(containers []corev1.Container, requested string) (*corev1.Container, error) {
	if requested != "" {
		for i := range containers {
			if containers[i].Name == requested {
				return &containers[i], nil
			}
		}
		return nil, fmt.Errorf("container %q not found in deployment", requested)
	}
	if len(containers) == 0 {
		return nil, fmt.Errorf("deployment has no containers")
	}
	if len(containers) > 1 {
		return nil, fmt.Errorf("deployment has multiple containers; specify container explicitly")
	}
	return &containers[0], nil
}

func deploymentStable(d *appsv1.Deployment, desired int32) (bool, string) {
	if desired == 0 {
		return true, ""
	}
	if d.Status.ObservedGeneration < d.Generation {
		return false, "deployment controller has not observed the latest generation yet"
	}
	if d.Status.UpdatedReplicas < desired {
		return false, "deployment rollout is still updating replicas"
	}
	if d.Status.ReadyReplicas < desired {
		return false, "deployment rollout is still waiting for ready replicas"
	}
	if d.Status.AvailableReplicas < desired {
		return false, "deployment rollout is still waiting for available replicas"
	}
	return true, ""
}

func deploymentDigest(ctx context.Context, kc kubernetes.Interface, namespace string, d *appsv1.Deployment, container string) (string, error) {
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return "", fmt.Errorf("deployment selector: %w", err)
	}
	if selector.Empty() {
		selector = labels.Everything()
	}
	list, err := kc.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "", fmt.Errorf("list deployment pods: %w", err)
	}
	unique := map[string]struct{}{}
	for _, pod := range list.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if !podReady(&pod) {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != container {
				continue
			}
			if digest := ExtractDigest(status.ImageID); digest != "" {
				unique[digest] = struct{}{}
			}
		}
	}
	if len(unique) == 1 {
		for digest := range unique {
			return digest, nil
		}
	}
	if len(unique) > 1 {
		return "", nil
	}
	for _, spec := range d.Spec.Template.Spec.Containers {
		if spec.Name == container {
			return ExtractDigest(spec.Image), nil
		}
	}
	return "", nil
}

func podReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func collectImagePullSecrets(ctx context.Context, kc kubernetes.Interface, namespace string, d *appsv1.Deployment) []string {
	names := map[string]struct{}{}
	for _, ref := range d.Spec.Template.Spec.ImagePullSecrets {
		if ref.Name != "" {
			names[ref.Name] = struct{}{}
		}
	}
	serviceAccount := d.Spec.Template.Spec.ServiceAccountName
	if serviceAccount == "" {
		serviceAccount = "default"
	}
	if sa, err := kc.CoreV1().ServiceAccounts(namespace).Get(ctx, serviceAccount, metav1.GetOptions{}); err == nil {
		for _, ref := range sa.ImagePullSecrets {
			if ref.Name != "" {
				names[ref.Name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func loadRegistryCredentials(ctx context.Context, kc kubernetes.Interface, namespace string, secretNames []string) ([]RegistryCredential, error) {
	if len(secretNames) == 0 {
		return nil, nil
	}
	credentials := []RegistryCredential{}
	seen := map[string]struct{}{}
	for _, secretName := range secretNames {
		if secretName == "" {
			continue
		}
		secret, err := kc.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("load image pull secret %s/%s: %w", namespace, secretName, err)
		}
		for _, credential := range credentialsFromSecret(secret) {
			key := credential.Registry + "|" + credential.Username + "|" + credential.Password
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			credentials = append(credentials, credential)
		}
	}
	return credentials, nil
}

func credentialsFromSecret(secret *corev1.Secret) []RegistryCredential {
	if secret == nil {
		return nil
	}
	var auths map[string]dockerConfigAuth
	switch {
	case len(secret.Data[corev1.DockerConfigJsonKey]) > 0:
		var cfg dockerConfigJSON
		if err := json.Unmarshal(secret.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
			return nil
		}
		auths = cfg.Auths
	case len(secret.Data[corev1.DockerConfigKey]) > 0:
		if err := json.Unmarshal(secret.Data[corev1.DockerConfigKey], &auths); err != nil {
			return nil
		}
	default:
		return nil
	}
	out := make([]RegistryCredential, 0, len(auths))
	for registry, auth := range auths {
		username, password := auth.Username, auth.Password
		if username == "" && password == "" && auth.Auth != "" {
			username, password = decodeDockerAuth(auth.Auth)
		}
		if username == "" && password == "" {
			continue
		}
		out = append(out, RegistryCredential{
			Registry: normalizeRegistryHost(registry),
			Username: username,
			Password: password,
		})
	}
	return out
}

func decodeDockerAuth(encoded string) (string, string) {
	if encoded == "" {
		return "", ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return "", ""
		}
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func normalizeRegistryHost(registry string) string {
	registry = strings.TrimSpace(registry)
	registry = strings.TrimPrefix(registry, "https://")
	registry = strings.TrimPrefix(registry, "http://")
	registry = strings.TrimSuffix(registry, "/")
	if idx := strings.Index(registry, "/"); idx >= 0 {
		registry = registry[:idx]
	}
	switch registry {
	case "docker.io", "registry-1.docker.io":
		return "index.docker.io"
	default:
		return registry
	}
}

func credentialForRegistry(registry string, credentials []RegistryCredential) authn.Authenticator {
	normalized := normalizeRegistryHost(registry)
	for _, credential := range credentials {
		if normalizeRegistryHost(credential.Registry) != normalized {
			continue
		}
		return authn.FromConfig(authn.AuthConfig{Username: credential.Username, Password: credential.Password})
	}
	return nil
}

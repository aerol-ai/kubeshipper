package rollout

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/store"
)

var ErrWatchNotFound = errors.New("rollout watch not found")

type Manager struct {
	store *store.Store
	kube  *kube.Client
	mu    sync.Mutex
}

type RegisterRequest struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment,omitempty"`
	Service    string `json:"service,omitempty"`
	Container  string `json:"container,omitempty"`
}

type RegisterResult struct {
	Created bool                `json:"created"`
	Watch   *store.RolloutWatch `json:"watch"`
}

type SyncResult struct {
	Applied bool                `json:"applied"`
	Result  string              `json:"result"`
	Message string              `json:"message,omitempty"`
	Watch   *store.RolloutWatch `json:"watch,omitempty"`
}

type WatchMutationResult struct {
	Message string              `json:"message,omitempty"`
	Watch   *store.RolloutWatch `json:"watch,omitempty"`
}

type DiscoveryTargetContainer struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	TrackedImage string `json:"tracked_image"`
}

type DiscoveryTarget struct {
	Namespace  string                     `json:"namespace"`
	Deployment string                     `json:"deployment"`
	Service    string                     `json:"service,omitempty"`
	Containers []DiscoveryTargetContainer `json:"containers"`
}

type DiscoveryResult struct {
	Namespaces []string          `json:"namespaces"`
	Targets    []DiscoveryTarget `json:"targets"`
}

func NewManager(st *store.Store, kc *kube.Client) *Manager {
	return &Manager{store: st, kube: kc}
}

func (m *Manager) DiscoverTargets(ctx context.Context, namespace string) (*DiscoveryResult, error) {
	targets, err := m.kube.ListManagedDeploymentTargets(ctx, namespace)
	if err != nil {
		return nil, err
	}

	namespaces := make([]string, 0, len(m.kube.Managed))
	for ns := range m.kube.Managed {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	out := make([]DiscoveryTarget, 0, len(targets))
	for _, target := range targets {
		next := DiscoveryTarget{
			Namespace:  target.Namespace,
			Deployment: target.Deployment,
			Service:    target.Service,
			Containers: make([]DiscoveryTargetContainer, 0, len(target.Containers)),
		}
		for _, container := range target.Containers {
			next.Containers = append(next.Containers, DiscoveryTargetContainer{
				Name:         container.Name,
				Image:        container.Image,
				TrackedImage: container.TrackedImage,
			})
		}
		out = append(out, next)
	}

	return &DiscoveryResult{Namespaces: namespaces, Targets: out}, nil
}

func (m *Manager) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	target := strings.TrimSpace(req.Deployment)
	if target == "" {
		target = strings.TrimSpace(req.Service)
	}
	if target == "" {
		return nil, fmt.Errorf("deployment or service is required")
	}
	state, err := m.kube.InspectDeploymentImage(ctx, req.Namespace, target, strings.TrimSpace(req.Container))
	if err != nil {
		return nil, err
	}
	watch, created, err := m.store.UpsertRolloutWatch(
		state.Namespace,
		state.Deployment,
		state.Container,
		state.TrackedImage,
		state.CurrentImage,
		state.CurrentDigest,
	)
	if err != nil {
		return nil, err
	}
	message := "registered deployment for automatic image digest checks"
	if !created {
		message = "updated deployment registration for automatic image digest checks"
	}
	if err := m.store.AppendRolloutWatchEvent(watch.ID, store.RolloutWatchEvent{
		Type:          "registered",
		Result:        "registered",
		Message:       message,
		CurrentImage:  state.CurrentImage,
		CurrentDigest: state.CurrentDigest,
		TS:            time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}
	watch, err = m.store.GetRolloutWatch(watch.ID)
	if err != nil {
		return nil, err
	}
	return &RegisterResult{Created: created, Watch: watch}, nil
}

func (m *Manager) Sync(ctx context.Context, id string) (*SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	watch, err := m.store.GetRolloutWatch(id)
	if err != nil {
		return nil, err
	}
	if watch == nil {
		return nil, ErrWatchNotFound
	}
	return m.syncWatch(ctx, watch)
}

func (m *Manager) SyncAll(ctx context.Context) {
	watches, err := m.store.ListRolloutWatches()
	if err != nil {
		log.Printf("rollout watch: list: %v", err)
		return
	}
	for _, watch := range watches {
		if !watch.Enabled {
			continue
		}
		if _, err := m.Sync(ctx, watch.ID); err != nil && !errors.Is(err, ErrWatchNotFound) {
			log.Printf("rollout watch: sync %s/%s: %v", watch.Namespace, watch.Deployment, err)
		}
	}
}

func (m *Manager) SetEnabled(ctx context.Context, id string, enabled bool) (*WatchMutationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	watch, err := m.store.GetRolloutWatch(id)
	if err != nil {
		return nil, err
	}
	if watch == nil {
		return nil, ErrWatchNotFound
	}
	if err := m.store.SetRolloutWatchEnabled(id, enabled); err != nil {
		return nil, err
	}
	result := "disabled"
	message := fmt.Sprintf("rollout watch disabled for %s/%s", watch.Namespace, watch.Deployment)
	if enabled {
		result = "enabled"
		message = fmt.Sprintf("rollout watch enabled for %s/%s", watch.Namespace, watch.Deployment)
	}
	if err := m.store.UpdateRolloutWatchResult(id, result, ""); err != nil {
		return nil, err
	}
	if err := m.store.AppendRolloutWatchEvent(id, store.RolloutWatchEvent{
		Type:    result,
		Result:  result,
		Message: message,
		TS:      time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}
	updated, err := m.store.GetRolloutWatch(id)
	if err != nil {
		return nil, err
	}
	return &WatchMutationResult{Message: message, Watch: updated}, nil
}

func (m *Manager) Restart(ctx context.Context, id string) (*WatchMutationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	watch, err := m.store.GetRolloutWatch(id)
	if err != nil {
		return nil, err
	}
	if watch == nil {
		return nil, ErrWatchNotFound
	}
	if err := m.kube.RestartService(ctx, watch.Deployment, watch.Namespace); err != nil {
		_ = m.store.UpdateRolloutWatchResult(id, "error", err.Error())
		_ = m.store.AppendRolloutWatchEvent(id, store.RolloutWatchEvent{
			Type:    "error",
			Result:  "error",
			Message: "failed to restart watched deployment",
			Error:   err.Error(),
			TS:      time.Now().UnixMilli(),
		})
		return nil, err
	}
	message := fmt.Sprintf("manual rollout restart requested for %s/%s", watch.Namespace, watch.Deployment)
	if err := m.store.UpdateRolloutWatchResult(id, "restarted", ""); err != nil {
		return nil, err
	}
	if err := m.store.AppendRolloutWatchEvent(id, store.RolloutWatchEvent{
		Type:    "restarted",
		Result:  "restarted",
		Message: message,
		TS:      time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}
	updated, err := m.store.GetRolloutWatch(id)
	if err != nil {
		return nil, err
	}
	return &WatchMutationResult{Message: message, Watch: updated}, nil
}

func (m *Manager) syncWatch(ctx context.Context, watch *store.RolloutWatch) (*SyncResult, error) {
	state, err := m.kube.InspectDeploymentImage(ctx, watch.Namespace, watch.Deployment, watch.Container)
	if err != nil {
		_ = m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
			Result:      "error",
			Message:     "failed to inspect deployment",
			Error:       err.Error(),
			RecordEvent: shouldRecordEvent(watch, "error", err.Error(), "", false),
			EventType:   "error",
		})
		return nil, err
	}

	if !state.Stable {
		message := state.Reason
		if message == "" {
			message = "deployment rollout is still in progress"
		}
		result := "deferred"
		if err := m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
			TrackedImage:  state.TrackedImage,
			Result:        result,
			Message:       message,
			CurrentImage:  state.CurrentImage,
			CurrentDigest: state.CurrentDigest,
			RecordEvent:   shouldRecordEvent(watch, result, "", "", false),
			EventType:     "deferred",
		}); err != nil {
			return nil, err
		}
		return m.result(watch.ID, false, result, message)
	}

	resolved, err := m.kube.ResolveLatestImage(ctx, state.Namespace, state.TrackedImage, state.PullSecrets)
	if err != nil {
		_ = m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
			TrackedImage:  state.TrackedImage,
			Result:        "error",
			Message:       "failed to resolve latest image digest",
			CurrentImage:  state.CurrentImage,
			CurrentDigest: state.CurrentDigest,
			Error:         err.Error(),
			RecordEvent:   shouldRecordEvent(watch, "error", err.Error(), "", false),
			EventType:     "error",
		})
		return nil, err
	}

	currentDigest := state.CurrentDigest
	if currentDigest == "" {
		currentDigest = kube.ExtractDigest(state.CurrentImage)
	}

	if currentDigest == "" && state.DesiredReplicas > 0 {
		message := "deployment image digest is not available yet"
		result := "deferred"
		if err := m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
			TrackedImage: state.TrackedImage,
			Result:       result,
			Message:      message,
			CurrentImage: state.CurrentImage,
			LatestImage:  resolved.Image,
			LatestDigest: resolved.Digest,
			RecordEvent:  shouldRecordEvent(watch, result, "", "", false),
			EventType:    "deferred",
		}); err != nil {
			return nil, err
		}
		return m.result(watch.ID, false, result, message)
	}

	upToDate := currentDigest != "" && currentDigest == resolved.Digest
	if state.DesiredReplicas == 0 && currentDigest == "" {
		upToDate = state.CurrentImage == resolved.Image
	}
	if upToDate {
		message := fmt.Sprintf("deployment already runs digest %s", resolved.Digest)
		if err := m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
			TrackedImage:  state.TrackedImage,
			Result:        "up_to_date",
			Message:       message,
			CurrentImage:  state.CurrentImage,
			CurrentDigest: currentDigest,
			LatestImage:   resolved.Image,
			LatestDigest:  resolved.Digest,
			RecordEvent:   false,
		}); err != nil {
			return nil, err
		}
		return m.result(watch.ID, false, "up_to_date", message)
	}

	if err := m.kube.UpdateDeploymentImage(ctx, state.Deployment, state.Namespace, state.Container, resolved.Image); err != nil {
		_ = m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
			TrackedImage:  state.TrackedImage,
			Result:        "error",
			Message:       "failed to patch deployment image",
			CurrentImage:  state.CurrentImage,
			CurrentDigest: currentDigest,
			LatestImage:   resolved.Image,
			LatestDigest:  resolved.Digest,
			Error:         err.Error(),
			RecordEvent:   shouldRecordEvent(watch, "error", err.Error(), resolved.Digest, false),
			EventType:     "error",
		})
		return nil, err
	}

	message := fmt.Sprintf("patched deployment image to %s", resolved.Image)
	if err := m.store.RecordRolloutWatchCheck(watch.ID, store.RolloutWatchCheck{
		TrackedImage:  state.TrackedImage,
		Result:        "updated",
		Message:       message,
		CurrentImage:  state.CurrentImage,
		CurrentDigest: currentDigest,
		LatestImage:   resolved.Image,
		LatestDigest:  resolved.Digest,
		Applied:       true,
		RecordEvent:   true,
		EventType:     "updated",
	}); err != nil {
		return nil, err
	}
	return m.result(watch.ID, true, "updated", message)
}

func (m *Manager) result(id string, applied bool, result, message string) (*SyncResult, error) {
	watch, err := m.store.GetRolloutWatch(id)
	if err != nil {
		return nil, err
	}
	return &SyncResult{Applied: applied, Result: result, Message: message, Watch: watch}, nil
}

func shouldRecordEvent(watch *store.RolloutWatch, result, errText, marker string, applied bool) bool {
	if applied {
		return true
	}
	if watch == nil {
		return true
	}
	if watch.LastResult != result {
		return true
	}
	if watch.LastError != errText {
		return true
	}
	return marker != "" && marker != watch.LatestDigest
}

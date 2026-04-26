package helm

import (
	"fmt"
	"os"
	"sync"

	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/store"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	helmkube "helm.sh/helm/v3/pkg/kube"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// Manager wraps the Helm SDK with our store + kube clients.
// All public entry points (Install, Upgrade, ...) emit progress via the
// supplied callback so the API layer can fan it out to SSE subscribers.
type Manager struct {
	settings *cli.EnvSettings
	mu       sync.Mutex
	kube     *kube.Client
	store    *store.Store
}

func New(kc *kube.Client, st *store.Store) (*Manager, error) {
	return &Manager{
		settings: cli.New(),
		kube:     kc,
		store:    st,
	}, nil
}

// EmitFn is called by long-running operations to publish progress events.
// Implementations should be non-blocking (e.g. push to a buffered channel).
type EmitFn func(ev store.Event)

// actionConfig builds a per-namespace action.Configuration. Helm requires
// this to be re-built per call because it embeds the namespace and storage backend.
func (m *Manager) actionConfig(namespace string) (*action.Configuration, error) {
	cfg := new(action.Configuration)

	flags := genericclioptions.NewConfigFlags(false)
	flags.Namespace = &namespace
	if v := os.Getenv("KUBECONFIG"); v != "" {
		flags.KubeConfig = &v
	}

	rcg := &restClientGetter{flags: flags, restCfg: m.kube.Cfg}

	if err := cfg.Init(rcg, namespace, "secret", logf); err != nil {
		return nil, fmt.Errorf("action config init: %w", err)
	}
	cfg.KubeClient = helmkube.New(rcg)
	return cfg, nil
}

func logf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, "helm: "+format+"\n", v...)
}

package server

import (
	"fmt"
	"os"
	"sync"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Server struct {
	pb.UnimplementedHelmdServer

	settings *cli.EnvSettings
	mu       sync.Mutex // serializes Helm operations per-process
	kc       kubernetes.Interface
	restCfg  *rest.Config
}

func New() (*Server, error) {
	settings := cli.New()

	cfg, err := loadKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("kube config: %w", err)
	}

	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube client: %w", err)
	}

	return &Server{
		settings: settings,
		kc:       kc,
		restCfg:  cfg,
	}, nil
}

func loadKubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

// actionConfig builds a per-namespace action.Configuration. Helm requires this
// to be re-built per call because it embeds the namespace and storage backend.
func (s *Server) actionConfig(namespace string) (*action.Configuration, error) {
	cfg := new(action.Configuration)

	flags := genericclioptions.NewConfigFlags(false)
	flags.Namespace = &namespace
	if v := os.Getenv("KUBECONFIG"); v != "" {
		flags.KubeConfig = &v
	}

	rcg := &restClientGetter{flags: flags, restCfg: s.restCfg}

	if err := cfg.Init(rcg, namespace, "secret", logf); err != nil {
		return nil, fmt.Errorf("action config init: %w", err)
	}

	// Use a kube.Client backed by our rest.Config so probes/wait work consistently.
	cfg.KubeClient = kube.New(rcg)
	return cfg, nil
}

func logf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, "helm: "+format+"\n", v...)
}

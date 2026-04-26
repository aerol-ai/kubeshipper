package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client bundles all kube clientsets the rest of the app needs.
// Created once at startup; safe for concurrent use.
type Client struct {
	Cfg     *rest.Config
	KC      kubernetes.Interface
	Managed map[string]bool
}

func New(managed map[string]bool) (*Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("kube config: %w", err)
	}
	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube client: %w", err)
	}
	return &Client{Cfg: cfg, KC: kc, Managed: managed}, nil
}

// ResolveNamespace defaults to the first allow-listed namespace if requested
// is empty, and rejects anything not in the allow-list.
func (c *Client) ResolveNamespace(requested string) (string, error) {
	if requested != "" {
		if !c.Managed[requested] {
			return "", fmt.Errorf("namespace %q is not in MANAGED_NAMESPACES", requested)
		}
		return requested, nil
	}
	for n := range c.Managed {
		return n, nil
	}
	return "", fmt.Errorf("no MANAGED_NAMESPACES configured")
}

func loadConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

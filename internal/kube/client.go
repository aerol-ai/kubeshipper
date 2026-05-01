package kube

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client bundles all kube clientsets the rest of the app needs.
// Created once at startup; safe for concurrent use.
type Client struct {
	Cfg           *rest.Config
	KC            kubernetes.Interface
	Managed       map[string]bool
	Wildcard      bool
	ImageResolver RegistryResolver
}

func New(managed map[string]bool, wildcard bool) (*Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("kube config: %w", err)
	}
	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube client: %w", err)
	}
	return &Client{Cfg: cfg, KC: kc, Managed: managed, Wildcard: wildcard, ImageResolver: defaultRegistryResolver{}}, nil
}

// ResolveNamespace defaults to the first allow-listed namespace if requested
// is empty, and rejects anything not in the allow-list. In wildcard mode any
// non-empty namespace is accepted; empty falls back to "default".
func (c *Client) ResolveNamespace(requested string) (string, error) {
	if c.Wildcard {
		if requested != "" {
			return requested, nil
		}
		return "default", nil
	}
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

// ListAvailableNamespaces returns the namespaces this client may operate on:
// the static allow-list, or all namespaces from the API in wildcard mode.
func (c *Client) ListAvailableNamespaces(ctx context.Context) ([]string, error) {
	if c.Wildcard {
		list, err := c.KC.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		out := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			out = append(out, ns.Name)
		}
		sort.Strings(out)
		return out, nil
	}
	out := make([]string, 0, len(c.Managed))
	for ns := range c.Managed {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out, nil
}

func loadConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

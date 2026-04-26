package helm

import (
	"context"
	"fmt"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// Diff compares Helm's stored manifest with the live cluster.
// Presence-only: any rendered resource that's missing from the cluster is drift.
// Field-level diffing intentionally out of scope for v1.
func (m *Manager) Diff(ctx context.Context, release, namespace string) (*DiffResp, error) {
	cfg, err := m.actionConfig(namespace)
	if err != nil {
		return nil, err
	}
	rel, err := action.NewGet(cfg).Run(release)
	if err != nil {
		return nil, fmt.Errorf("get release: %w", err)
	}

	dyn, err := dynamic.NewForConfig(m.kube.Cfg)
	if err != nil {
		return nil, err
	}
	mapper, err := (&restClientGetter{restCfg: m.kube.Cfg}).ToRESTMapper()
	if err != nil {
		return nil, err
	}

	out := &DiffResp{}
	for _, doc := range splitYAML(rel.Manifest) {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil || obj == nil {
			continue
		}
		api, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		md, _ := obj["metadata"].(map[string]any)
		name, _ := md["name"].(string)
		ns, _ := md["namespace"].(string)
		if name == "" {
			continue
		}

		gv, err := schema.ParseGroupVersion(api)
		if err != nil {
			continue
		}
		mapping, err := mapper.RESTMapping(gv.WithKind(kind).GroupKind(), gv.Version)
		if err != nil {
			continue
		}

		var ri dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ri = dyn.Resource(mapping.Resource).Namespace(ns)
		} else {
			ri = dyn.Resource(mapping.Resource)
		}

		_, err = ri.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) || strings.Contains(err.Error(), "not found") {
				out.Drifted = true
				out.Entries = append(out.Entries, DiffEntry{
					Kind: kind, Name: name, Namespace: ns,
					Change: "removed", Detail: "missing in cluster",
				})
			}
		}
	}
	return out, nil
}

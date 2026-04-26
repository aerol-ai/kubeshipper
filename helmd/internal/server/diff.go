package server

import (
	"context"
	"fmt"
	"strings"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// Diff compares Helm's desired manifest for a release against the live cluster.
// Naive comparator: presence-only — any missing resource counts as drift.
// Field-level diffing intentionally out of scope for v1.
func (s *Server) Diff(ctx context.Context, req *pb.DiffRequest) (*pb.DiffResponse, error) {
	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return nil, err
	}
	get := action.NewGet(cfg)
	rel, err := get.Run(req.Release)
	if err != nil {
		return nil, fmt.Errorf("get release: %w", err)
	}

	dyn, err := dynamic.NewForConfig(s.restCfg)
	if err != nil {
		return nil, err
	}

	mapper, err := (&restClientGetter{restCfg: s.restCfg}).ToRESTMapper()
	if err != nil {
		return nil, err
	}

	out := &pb.DiffResponse{}
	for _, doc := range splitYAML(rel.Manifest) {
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil || m == nil {
			continue
		}
		api, _ := m["apiVersion"].(string)
		kind, _ := m["kind"].(string)
		md, _ := m["metadata"].(map[string]interface{})
		name, _ := md["name"].(string)
		ns, _ := md["namespace"].(string)
		if name == "" {
			continue
		}

		gv, err := schema.ParseGroupVersion(api)
		if err != nil {
			continue
		}
		gvk := gv.WithKind(kind)

		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			continue
		}

		var ri dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ri = dyn.Resource(mapping.Resource).Namespace(ns)
		} else {
			ri = dyn.Resource(mapping.Resource)
		}

		_, err = ri.Get(ctx, name, getOptions())
		if err != nil {
			if isNotFound(err) {
				out.Drifted = true
				out.Entries = append(out.Entries, &pb.DiffEntry{
					Kind: kind, Name: name, Namespace: ns,
					Change: "removed",
					Detail: "resource missing in cluster",
				})
				continue
			}
		}
	}
	return out, nil
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

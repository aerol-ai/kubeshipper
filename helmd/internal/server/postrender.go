package server

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	pb "kubeshipper/helmd/gen"
	"kubeshipper/helmd/internal/source"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/postrender"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// disabledStore is an in-memory record of resources stripped from a release.
// The TS layer also persists this in SQLite (source of truth across restarts);
// helmd reads back from the TS layer via gRPC at boot. For simplicity in v1
// we keep an in-memory mirror here that the TS layer pushes to.
type disabledStore struct {
	mu sync.RWMutex
	// key: release/namespace
	m map[string][]*pb.DisabledResource
}

var disabled = &disabledStore{m: map[string][]*pb.DisabledResource{}}

func key(release, ns string) string { return ns + "/" + release }

func (s *Server) listDisabled(release, ns string) []*pb.DisabledResource {
	disabled.mu.RLock()
	defer disabled.mu.RUnlock()
	out := make([]*pb.DisabledResource, len(disabled.m[key(release, ns)]))
	copy(out, disabled.m[key(release, ns)])
	return out
}

func (s *Server) addDisabled(release, ns string, r *pb.DisabledResource) {
	disabled.mu.Lock()
	defer disabled.mu.Unlock()
	k := key(release, ns)
	disabled.m[k] = append(disabled.m[k], r)
}

func (s *Server) removeDisabled(release, ns string, r *pb.DisabledResource) {
	disabled.mu.Lock()
	defer disabled.mu.Unlock()
	k := key(release, ns)
	out := disabled.m[k][:0]
	for _, d := range disabled.m[k] {
		if d.Kind == r.Kind && d.Name == r.Name && d.Namespace == r.Namespace {
			continue
		}
		out = append(out, d)
	}
	disabled.m[k] = out
}

// resourceFilter implements postrender.PostRenderer.
// It drops any rendered manifest whose kind+name+namespace matches the disabled list.
type resourceFilter struct {
	skip []*pb.DisabledResource
}

func (f *resourceFilter) Run(in *bytes.Buffer) (*bytes.Buffer, error) {
	out := &bytes.Buffer{}
	docs := splitYAML(in.String())
	first := true
	for _, doc := range docs {
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil || m == nil {
			continue
		}
		kind, _ := m["kind"].(string)
		md, _ := m["metadata"].(map[string]interface{})
		name, _ := md["name"].(string)
		ns, _ := md["namespace"].(string)

		drop := false
		for _, sk := range f.skip {
			if sk.Kind == kind && sk.Name == name &&
				(sk.Namespace == "" || sk.Namespace == ns) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		if !first {
			out.WriteString("\n---\n")
		}
		out.WriteString(doc)
		first = false
	}
	return out, nil
}

// postRendererFor returns a PostRenderer for the given release if any resources are disabled, else nil.
func (s *Server) postRendererFor(release, ns string) (postrender.PostRenderer, error) {
	skip := s.listDisabled(release, ns)
	if len(skip) == 0 {
		return nil, nil
	}
	return &resourceFilter{skip: skip}, nil
}

func (s *Server) DisableResource(req *pb.DisableResourceRequest, stream pb.Helmd_DisableResourceServer) error {
	emit := newEmitter(stream)
	if req.Resource == nil || req.Resource.Kind == "" || req.Resource.Name == "" {
		return emitError(stream, "resource.kind and resource.name are required")
	}

	emit("validation", fmt.Sprintf("disabling %s/%s in release %s/%s",
		req.Resource.Kind, req.Resource.Name, req.Namespace, req.Release))

	s.addDisabled(req.Release, req.Namespace, req.Resource)

	// Trigger an upgrade with the post-renderer.
	upReq := &pb.UpgradeRequest{
		Release:        req.Release,
		Namespace:      req.Namespace,
		Source:         req.Source,
		ValuesYaml:     req.ValuesYaml,
		Atomic:         true,
		Wait:           true,
		ReuseValues:    true,
		TimeoutSeconds: req.TimeoutSeconds,
	}
	if err := s.runUpgrade(stream.Context(), upReq, emit); err != nil {
		return emitError(stream, err.Error())
	}

	if req.DeletePvcs {
		if del, err := s.sweepResourcePVCs(stream.Context(), req.Resource); err != nil {
			emit("apply", "pvc sweep error: "+err.Error())
		} else if len(del) > 0 {
			emit("apply", fmt.Sprintf("deleted %d pvc(s) bound to %s/%s", len(del), req.Resource.Kind, req.Resource.Name))
		}
	}

	emit("done", "resource disabled")
	return nil
}

func (s *Server) EnableResource(req *pb.EnableResourceRequest, stream pb.Helmd_EnableResourceServer) error {
	emit := newEmitter(stream)
	if req.Resource == nil {
		return emitError(stream, "resource is required")
	}
	s.removeDisabled(req.Release, req.Namespace, req.Resource)

	upReq := &pb.UpgradeRequest{
		Release:        req.Release,
		Namespace:      req.Namespace,
		Source:         req.Source,
		ValuesYaml:     req.ValuesYaml,
		Atomic:         true,
		Wait:           true,
		ReuseValues:    true,
		TimeoutSeconds: req.TimeoutSeconds,
	}
	if err := s.runUpgrade(stream.Context(), upReq, emit); err != nil {
		return emitError(stream, err.Error())
	}
	emit("done", "resource enabled")
	return nil
}

// runUpgrade is a helper that performs an upgrade with progress emission, used by Disable/Enable.
func (s *Server) runUpgrade(ctx context.Context, req *pb.UpgradeRequest, emit func(phase, msg string)) error {
	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return err
	}
	ch, err := source.Fetch(req.Source)
	if err != nil {
		return fmt.Errorf("fetch chart: %w", err)
	}
	values, err := parseValues(req.ValuesYaml)
	if err != nil {
		return err
	}

	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = req.Namespace
	upgrade.Atomic = req.Atomic
	upgrade.Wait = req.Wait
	upgrade.ReuseValues = req.ReuseValues
	upgrade.Timeout = timeout(req.TimeoutSeconds, 10*time.Minute)

	pr, err := s.postRendererFor(req.Release, req.Namespace)
	if err != nil {
		return err
	}
	if pr != nil {
		upgrade.PostRenderer = pr
	}

	emit("apply", "running helm upgrade with post-renderer")
	rel, err := upgrade.RunWithContext(ctx, req.Release, ch, values)
	if err != nil {
		return fmt.Errorf("upgrade: %w", err)
	}
	emit("done", fmt.Sprintf("revision=%d status=%s", rel.Version, rel.Info.Status))
	return nil
}

// sweepResourcePVCs deletes PVCs whose owner-by-name matches a Deployment or StatefulSet.
// For StatefulSets the PVCs are named "<vct>-<sts>-<ordinal>"; we match by prefix.
func (s *Server) sweepResourcePVCs(ctx context.Context, r *pb.DisabledResource) ([]string, error) {
	if r.Namespace == "" {
		return nil, nil
	}
	pvcs, err := s.kc.CoreV1().PersistentVolumeClaims(r.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	deleted := []string{}
	for _, pvc := range pvcs.Items {
		match := false
		switch r.Kind {
		case "StatefulSet":
			match = strings.HasSuffix(pvc.Name, "-"+r.Name+"-0") ||
				strings.Contains(pvc.Name, "-"+r.Name+"-")
		case "Deployment":
			if v, ok := pvc.Labels["app"]; ok && v == r.Name {
				match = true
			}
		}
		if match {
			if err := s.kc.CoreV1().PersistentVolumeClaims(r.Namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err == nil {
				deleted = append(deleted, r.Namespace+"/"+pvc.Name)
			}
		}
	}
	return deleted, nil
}

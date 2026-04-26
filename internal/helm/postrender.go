package helm

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/store"

	"helm.sh/helm/v3/pkg/postrender"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// resourceFilter is a Helm PostRenderer that drops manifests whose
// kind+name+namespace match the disabled list for a given release.
type resourceFilter struct {
	skip []store.DisabledResource
}

func (f *resourceFilter) Run(in *bytes.Buffer) (*bytes.Buffer, error) {
	out := &bytes.Buffer{}
	first := true
	for _, doc := range splitYAML(in.String()) {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil || m == nil {
			continue
		}
		kind, _ := m["kind"].(string)
		md, _ := m["metadata"].(map[string]any)
		name, _ := md["name"].(string)
		ns, _ := md["namespace"].(string)

		drop := false
		for _, sk := range f.skip {
			if sk.Kind == kind && sk.Name == name &&
				(sk.ResourceNs == "" || sk.ResourceNs == ns) {
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

// postRendererFor returns a PostRenderer if the release has any disabled resources, else nil.
func (m *Manager) postRendererFor(release, namespace string) (postrender.PostRenderer, error) {
	rows, err := m.store.ListDisabled(release, namespace)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &resourceFilter{skip: rows}, nil
}

// DisableResource records a resource as disabled, then runs an upgrade with
// the post-renderer so the disabled resource is removed from the cluster.
// Optionally sweeps PVCs that look bound to the disabled resource.
func (m *Manager) DisableResource(ctx context.Context, release, namespace, kind, name, resourceNs string,
	src *ChartSource, values map[string]any, deletePVCs bool, timeoutSec int, emit EmitFn) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	if kind == "" || name == "" {
		emitErrFn(emit, "kind and name are required")
		return fmt.Errorf("kind and name required")
	}

	emitFn(emit, "validation", fmt.Sprintf("disabling %s/%s in release %s/%s", kind, name, namespace, release))
	if err := m.store.RecordDisabled(release, namespace, kind, name, resourceNs); err != nil {
		emitErrFn(emit, fmt.Sprintf("record disabled: %v", err))
		return err
	}

	upReq := &UpgradeReq{
		Source:         src,
		Values:         values,
		Atomic:         ptrBool(true),
		Wait:           ptrBool(true),
		ReuseValues:    true,
		TimeoutSeconds: timeoutSec,
	}
	if err := m.runUpgradeLocked(ctx, release, namespace, upReq, emit); err != nil {
		return err
	}

	if deletePVCs && resourceNs != "" {
		if del, err := m.sweepResourcePVCs(ctx, kind, name, resourceNs); err != nil {
			emitFn(emit, "apply", "pvc sweep error: "+err.Error())
		} else if len(del) > 0 {
			emitFn(emit, "apply", fmt.Sprintf("deleted %d pvc(s)", len(del)))
		}
	}

	emitFn(emit, "done", "resource disabled")
	return nil
}

func (m *Manager) EnableResource(ctx context.Context, release, namespace, kind, name, resourceNs string,
	src *ChartSource, values map[string]any, timeoutSec int, emit EmitFn) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.store.ClearDisabled(release, namespace, kind, name, resourceNs); err != nil {
		emitErrFn(emit, fmt.Sprintf("clear disabled: %v", err))
		return err
	}
	upReq := &UpgradeReq{
		Source: src, Values: values,
		Atomic: ptrBool(true), Wait: ptrBool(true),
		ReuseValues: true, TimeoutSeconds: timeoutSec,
	}
	if err := m.runUpgradeLocked(ctx, release, namespace, upReq, emit); err != nil {
		return err
	}
	emitFn(emit, "done", "resource enabled")
	return nil
}

func (m *Manager) sweepResourcePVCs(ctx context.Context, kind, name, ns string) ([]string, error) {
	pvcs, err := m.kube.KC.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	deleted := []string{}
	for _, pvc := range pvcs.Items {
		match := false
		switch kind {
		case "StatefulSet":
			match = strings.Contains(pvc.Name, "-"+name+"-")
		case "Deployment":
			if v, ok := pvc.Labels["app"]; ok && v == name {
				match = true
			}
		}
		if match {
			if err := m.kube.KC.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err == nil {
				deleted = append(deleted, ns+"/"+pvc.Name)
			}
		}
	}
	return deleted, nil
}

func ptrBool(b bool) *bool { return &b }

var _ = time.Second // ensure time import has a referent

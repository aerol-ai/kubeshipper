package helm

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm/source"

	"helm.sh/helm/v3/pkg/action"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Preflight inspects a chart spec without applying anything.
// Returns a list of checks and a top-level OK flag (true iff no blocking failures).
func (m *Manager) Preflight(ctx context.Context, req *PreflightReq) (*PreflightResp, error) {
	checks := []PreflightCheck{}
	add := func(name string, blocking, passed bool, msg string) {
		checks = append(checks, PreflightCheck{Name: name, Blocking: blocking, Passed: passed, Message: msg})
	}

	manifest, requiredCRDs, err := m.renderForPreflight(ctx, req)
	if err != nil {
		add("chart-resolvable", true, false, err.Error())
		return &PreflightResp{OK: false, Checks: checks}, nil
	}
	add("chart-resolvable", true, true, "chart pulled and rendered")

	apiext, _ := apiextclient.NewForConfig(m.kube.Cfg)
	for _, name := range requiredCRDs {
		ok := false
		if apiext != nil {
			_, err := apiext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
			ok = err == nil
		}
		add("crd:"+name, true, ok, fmt.Sprintf("CRD %s installed: %v", name, ok))
	}

	if hasPVC(manifest) {
		ok, msg := m.defaultStorageClassPresent(ctx)
		add("default-storage-class", true, ok, msg)
	}

	if cfg, err := m.actionConfig(req.Namespace); err == nil {
		list := action.NewList(cfg)
		list.Filter = req.Release
		rels, _ := list.Run()
		add("no-conflicting-release", true, len(rels) == 0,
			fmt.Sprintf("found %d existing release(s) named %q in %s", len(rels), req.Release, req.Namespace))
	}

	for _, host := range scanIngressHosts(manifest) {
		ok := dnsResolves(host, 2*time.Second)
		add("dns:"+host, false, ok, fmt.Sprintf("DNS for %s resolves: %v", host, ok))
	}

	out := &PreflightResp{Checks: checks, OK: true}
	for _, c := range checks {
		if c.Blocking && !c.Passed {
			out.OK = false
			break
		}
	}
	return out, nil
}

func (m *Manager) renderForPreflight(ctx context.Context, req *PreflightReq) (string, []string, error) {
	cfg, err := m.actionConfig(req.Namespace)
	if err != nil {
		return "", nil, err
	}
	ch, err := source.Fetch(toSourceReq(req.Source))
	if err != nil {
		return "", nil, err
	}
	valuesYAML, _ := valuesToYAML(req.Values)
	values, _ := parseValuesYAML(valuesYAML)

	install := action.NewInstall(cfg)
	install.DryRun = true
	install.ClientOnly = true
	install.Replace = true
	install.ReleaseName = req.Release
	install.Namespace = req.Namespace
	rel, err := install.RunWithContext(ctx, ch, values)
	if err != nil {
		return "", nil, err
	}
	return rel.Manifest, scanCRDs(rel.Manifest), nil
}

func (m *Manager) defaultStorageClassPresent(ctx context.Context) (bool, string) {
	scs, err := m.kube.KC.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, "list storageclass: " + err.Error()
	}
	for _, sc := range scs.Items {
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			return true, "default StorageClass: " + sc.Name
		}
	}
	return false, "no default StorageClass found"
}

func hasPVC(manifest string) bool {
	return strings.Contains(manifest, "kind: PersistentVolumeClaim") ||
		strings.Contains(manifest, "volumeClaimTemplates")
}

func scanCRDs(manifest string) []string {
	seen := map[string]bool{}
	for _, doc := range splitYAML(manifest) {
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil || m == nil {
			continue
		}
		api, _ := m["apiVersion"].(string)
		kind, _ := m["kind"].(string)
		switch {
		case api == "cert-manager.io/v1" && kind == "Certificate":
			seen["certificates.cert-manager.io"] = true
		case api == "cert-manager.io/v1" && kind == "ClusterIssuer":
			seen["clusterissuers.cert-manager.io"] = true
		case api == "cert-manager.io/v1" && kind == "Issuer":
			seen["issuers.cert-manager.io"] = true
		case strings.HasPrefix(api, "traefik.io/") && kind == "IngressRoute":
			seen["ingressroutes.traefik.io"] = true
		case strings.HasPrefix(api, "traefik.io/") && kind == "Middleware":
			seen["middlewares.traefik.io"] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func splitYAML(s string) []string {
	parts := strings.Split(s, "\n---")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func scanIngressHosts(manifest string) []string {
	hosts := map[string]bool{}
	for _, doc := range splitYAML(manifest) {
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			continue
		}
		spec, ok := m["spec"].(map[string]interface{})
		if !ok {
			continue
		}
		if routes, ok := spec["routes"].([]interface{}); ok {
			for _, r := range routes {
				rm, _ := r.(map[string]interface{})
				if match, _ := rm["match"].(string); match != "" {
					for _, h := range extractHosts(match) {
						hosts[h] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(hosts))
	for h := range hosts {
		out = append(out, h)
	}
	return out
}

func extractHosts(match string) []string {
	out := []string{}
	for _, frag := range strings.Split(match, "||") {
		frag = strings.TrimSpace(frag)
		if i := strings.Index(frag, "Host(`"); i >= 0 {
			rest := frag[i+6:]
			if j := strings.Index(rest, "`"); j > 0 {
				out = append(out, rest[:j])
			}
		}
	}
	return out
}

func dnsResolves(host string, timeout time.Duration) bool {
	r := &net.Resolver{}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	addrs, err := r.LookupHost(ctx, host)
	return err == nil && len(addrs) > 0
}

// Render returns the rendered manifest of the chart for the given source/values.
// Used by the API GET /charts/preflight render-only path.
func (m *Manager) Render(ctx context.Context, req *PreflightReq) (string, error) {
	manifest, _, err := m.renderForPreflight(ctx, req)
	return manifest, err
}

package helm

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type UninstallResp struct {
	OK          bool     `json:"ok"`
	Message     string   `json:"message,omitempty"`
	DeletedPVCs []string `json:"deleted_pvcs,omitempty"`
}

func (m *Manager) Uninstall(ctx context.Context, release, namespace string, deletePVCs bool, timeoutSec int) (*UninstallResp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := m.actionConfig(namespace)
	if err != nil {
		return nil, fmt.Errorf("action config: %w", err)
	}

	un := action.NewUninstall(cfg)
	un.Wait = true
	un.Timeout = timeoutOrDefault(timeoutSec, 5*time.Minute)

	resp, err := un.Run(release)
	if err != nil {
		return nil, fmt.Errorf("uninstall: %w", err)
	}
	out := &UninstallResp{OK: true, Message: resp.Info}

	if deletePVCs {
		deleted, err := m.sweepReleasePVCs(ctx, release)
		if err != nil {
			out.Message += "; pvc sweep error: " + err.Error()
		} else {
			out.DeletedPVCs = deleted
		}
	}
	return out, nil
}

func (m *Manager) sweepReleasePVCs(ctx context.Context, release string) ([]string, error) {
	selector := "app.kubernetes.io/instance=" + release
	deleted := []string{}

	nss, err := m.kube.KC.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, ns := range nss.Items {
		pvcs, err := m.kube.KC.CoreV1().PersistentVolumeClaims(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			continue
		}
		for _, pvc := range pvcs.Items {
			if err := m.kube.KC.CoreV1().PersistentVolumeClaims(ns.Name).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err == nil {
				deleted = append(deleted, ns.Name+"/"+pvc.Name)
			}
		}
	}
	return deleted, nil
}

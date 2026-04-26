package server

import (
	"context"
	"fmt"
	"time"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/action"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) Uninstall(ctx context.Context, req *pb.UninstallRequest) (*pb.UninstallResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return nil, fmt.Errorf("action config: %w", err)
	}

	uninstall := action.NewUninstall(cfg)
	uninstall.KeepHistory = req.KeepHistory
	uninstall.Timeout = timeout(req.TimeoutSeconds, 5*time.Minute)
	uninstall.Wait = true

	resp, err := uninstall.Run(req.Release)
	if err != nil {
		return nil, fmt.Errorf("uninstall: %w", err)
	}

	out := &pb.UninstallResponse{
		Ok:      true,
		Message: resp.Info,
	}

	if req.DeletePvcs {
		deleted, err := s.sweepPVCs(ctx, req.Release)
		if err != nil {
			out.Message = out.Message + fmt.Sprintf("; pvc sweep failed: %v", err)
		} else {
			out.DeletedPvcs = deleted
		}
	}

	return out, nil
}

// sweepPVCs deletes PVCs whose label `app.kubernetes.io/instance=<release>` matches.
// Helm sets this on chart-managed PVCs by default via the recommended labels block.
func (s *Server) sweepPVCs(ctx context.Context, release string) ([]string, error) {
	selector := fmt.Sprintf("app.kubernetes.io/instance=%s", release)
	deleted := []string{}

	namespaces, err := s.kc.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces.Items {
		pvcs, err := s.kc.CoreV1().PersistentVolumeClaims(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			continue
		}
		for _, pvc := range pvcs.Items {
			if err := s.kc.CoreV1().PersistentVolumeClaims(ns.Name).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err == nil {
				deleted = append(deleted, ns.Name+"/"+pvc.Name)
			}
		}
	}
	return deleted, nil
}

package server

import (
	"context"
	"fmt"
	"time"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/action"
)

func (s *Server) Rollback(ctx context.Context, req *pb.RollbackRequest) (*pb.RollbackResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return nil, err
	}

	rb := action.NewRollback(cfg)
	rb.Version = int(req.Revision)
	rb.Wait = req.Wait
	rb.Timeout = timeout(req.TimeoutSeconds, 5*time.Minute)

	if err := rb.Run(req.Release); err != nil {
		return nil, fmt.Errorf("rollback: %w", err)
	}

	get := action.NewGet(cfg)
	rel, err := get.Run(req.Release)
	if err != nil {
		return nil, err
	}
	return &pb.RollbackResponse{Ok: true, NewRevision: int32(rel.Version)}, nil
}

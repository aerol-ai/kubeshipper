package server

import (
	"context"
	"fmt"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/action"
	"sigs.k8s.io/yaml"
)

func (s *Server) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return nil, err
	}

	get := action.NewGet(cfg)
	rel, err := get.Run(req.Release)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}

	valuesYAML, err := yaml.Marshal(rel.Config)
	if err != nil {
		return nil, err
	}

	disabled := s.listDisabled(req.Release, req.Namespace)

	return &pb.GetResponse{
		Release:    toPB(rel),
		ValuesYaml: string(valuesYAML),
		Manifest:   rel.Manifest,
		Disabled:   disabled,
	}, nil
}

func (s *Server) History(ctx context.Context, req *pb.HistoryRequest) (*pb.HistoryResponse, error) {
	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return nil, err
	}

	hist := action.NewHistory(cfg)
	max := int(req.Max)
	if max <= 0 {
		max = 20
	}
	hist.Max = max

	rels, err := hist.Run(req.Release)
	if err != nil {
		return nil, err
	}

	out := &pb.HistoryResponse{}
	for _, r := range rels {
		entry := &pb.HistoryEntry{
			Revision:    int32(r.Version),
			Status:      r.Info.Status.String(),
			UpdatedAt:   r.Info.LastDeployed.String(),
			Description: r.Info.Description,
		}
		if r.Chart != nil && r.Chart.Metadata != nil {
			entry.Chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
			entry.AppVersion = r.Chart.Metadata.AppVersion
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

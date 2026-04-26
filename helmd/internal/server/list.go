package server

import (
	"context"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
)

func (s *Server) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return nil, err
	}

	list := action.NewList(cfg)
	list.AllNamespaces = req.Namespace == ""
	list.All = req.All
	list.SetStateMask()

	rels, err := list.Run()
	if err != nil {
		return nil, err
	}

	out := &pb.ListResponse{}
	for _, r := range rels {
		out.Releases = append(out.Releases, toPB(r))
	}
	return out, nil
}

func toPB(r *release.Release) *pb.Release {
	chartName := ""
	appVersion := ""
	if r.Chart != nil && r.Chart.Metadata != nil {
		chartName = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
		appVersion = r.Chart.Metadata.AppVersion
	}
	return &pb.Release{
		Name:       r.Name,
		Namespace:  r.Namespace,
		Revision:   int32(r.Version),
		Status:     r.Info.Status.String(),
		Chart:      chartName,
		AppVersion: appVersion,
		UpdatedAt:  r.Info.LastDeployed.String(),
	}
}

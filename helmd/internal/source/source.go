package source

import (
	"fmt"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/chart"
)

// Fetch resolves any supported chart source into an in-memory *chart.Chart.
// Per-request credentials live on the source object and are never persisted.
func Fetch(src *pb.ChartSource) (*chart.Chart, error) {
	if src == nil {
		return nil, fmt.Errorf("chart source required")
	}
	switch s := src.Source.(type) {
	case *pb.ChartSource_Oci:
		return fetchOCI(s.Oci)
	case *pb.ChartSource_Https:
		return fetchHTTPS(s.Https)
	case *pb.ChartSource_Git:
		return fetchGit(s.Git)
	case *pb.ChartSource_Tgz:
		return fetchTgz(s.Tgz)
	default:
		return nil, fmt.Errorf("unsupported chart source")
	}
}

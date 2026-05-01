package source

import (
	"fmt"

	"helm.sh/helm/v3/pkg/chart"
)

// Req is the source-resolver-internal type the helm package translates to
// before calling Fetch. Decouples the source package from the larger types.go.
type Req struct {
	Type    string
	URL     string
	RepoURL string
	Chart   string
	Version string
	Ref     string
	Path    string
	TgzB64  string
	Auth    *Auth
}

type Auth struct {
	Username  string
	Password  string
	SshKeyPem string
	Token     string
}

// Fetch resolves any supported chart source into an in-memory *chart.Chart.
func Fetch(req *Req) (*chart.Chart, error) {
	if req == nil {
		return nil, fmt.Errorf("chart source required")
	}
	switch req.Type {
	case "oci":
		return fetchOCI(req)
	case "https":
		return fetchHTTPS(req)
	case "git":
		return fetchGit(req)
	case "tgz":
		return fetchTgz(req)
	default:
		return nil, fmt.Errorf("unsupported chart source type %q", req.Type)
	}
}

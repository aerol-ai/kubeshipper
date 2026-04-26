package helm

import "github.com/aerol-ai/kubeshipper/internal/helm/source"

// toSourceReq translates the public ChartSource (used by HTTP handlers) into
// the source-package internal Req. Keeps the source resolvers free of helm-package types.
func toSourceReq(s *ChartSource) *source.Req {
	if s == nil {
		return nil
	}
	r := &source.Req{
		Type: s.Type, URL: s.URL,
		RepoURL: s.RepoURL, Chart: s.Chart, Version: s.Version,
		Ref: s.Ref, Path: s.Path, TgzB64: s.TgzB64,
	}
	if s.Auth != nil {
		r.Auth = &source.Auth{
			Username:  s.Auth.Username,
			Password:  s.Auth.Password,
			SshKeyPem: s.Auth.SshKeyPem,
			Token:     s.Auth.Token,
		}
	}
	return r
}

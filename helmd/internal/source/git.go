package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "kubeshipper/helmd/gen"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

func fetchGit(s *pb.GitSource) (*chart.Chart, error) {
	if s == nil || s.RepoUrl == "" {
		return nil, fmt.Errorf("git.repo_url required")
	}

	dir, err := os.MkdirTemp("", "helmd-git-")
	if err != nil {
		return nil, err
	}
	// Caller is responsible for chart liveness — we keep the dir until load.

	cloneOpts := &git.CloneOptions{
		URL:      s.RepoUrl,
		Depth:    1,
		Progress: os.Stderr,
	}

	if s.Auth != nil {
		switch {
		case s.Auth.Token != "":
			cloneOpts.Auth = &githttp.BasicAuth{
				Username: "x-access-token",
				Password: s.Auth.Token,
			}
		case s.Auth.SshKeyPem != "":
			pubKeys, err := gitssh.NewPublicKeys("git", []byte(s.Auth.SshKeyPem), "")
			if err != nil {
				return nil, fmt.Errorf("ssh key: %w", err)
			}
			cloneOpts.Auth = pubKeys
		}
	}

	if s.Ref != "" {
		// Try as a branch first. If it fails, try as a tag, then as a sha.
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(s.Ref)
		cloneOpts.SingleBranch = true
	}

	repo, err := git.PlainClone(dir, false, cloneOpts)
	if err != nil && strings.Contains(err.Error(), "couldn't find remote ref") {
		// Retry as tag.
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(s.Ref)
		_ = os.RemoveAll(dir)
		dir, _ = os.MkdirTemp("", "helmd-git-")
		repo, err = git.PlainClone(dir, false, cloneOpts)
	}
	if err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}
	_ = repo

	chartPath := dir
	if s.Path != "" {
		chartPath = filepath.Join(dir, s.Path)
	}

	c, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart at %s: %w", chartPath, err)
	}
	_ = os.RemoveAll(dir)
	return c, nil
}

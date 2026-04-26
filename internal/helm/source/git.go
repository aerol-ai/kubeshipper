package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

func fetchGit(s *Req) (*chart.Chart, error) {
	if s.RepoURL == "" {
		return nil, fmt.Errorf("git.repoUrl required")
	}

	dir, err := os.MkdirTemp("", "kubeshipper-git-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	cloneOpts := &git.CloneOptions{
		URL:      s.RepoURL,
		Depth:    1,
		Progress: os.Stderr,
	}

	if s.Auth != nil {
		switch {
		case s.Auth.Token != "":
			cloneOpts.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: s.Auth.Token}
		case s.Auth.SshKeyPem != "":
			pubKeys, err := gitssh.NewPublicKeys("git", []byte(s.Auth.SshKeyPem), "")
			if err != nil {
				return nil, fmt.Errorf("ssh key: %w", err)
			}
			cloneOpts.Auth = pubKeys
		}
	}

	if s.Ref != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(s.Ref)
		cloneOpts.SingleBranch = true
	}

	if _, err := git.PlainClone(dir, false, cloneOpts); err != nil {
		// Branch ref miss → retry as tag.
		if strings.Contains(err.Error(), "couldn't find remote ref") && s.Ref != "" {
			cloneOpts.ReferenceName = plumbing.NewTagReferenceName(s.Ref)
			_ = os.RemoveAll(dir)
			dir, _ = os.MkdirTemp("", "kubeshipper-git-")
			if _, err := git.PlainClone(dir, false, cloneOpts); err != nil {
				return nil, fmt.Errorf("clone (tag): %w", err)
			}
		} else {
			return nil, fmt.Errorf("clone: %w", err)
		}
	}

	chartPath := dir
	if s.Path != "" {
		chartPath = filepath.Join(dir, s.Path)
	}
	c, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart at %s: %w", chartPath, err)
	}
	return c, nil
}

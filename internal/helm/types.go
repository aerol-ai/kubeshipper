package helm

// All chart-source / install / upgrade / etc. request shapes used by both the
// HTTP layer and the Helm manager. Defined here (no proto, no codegen) so the
// API boundary is one Go-typed contract.

import "encoding/json"

type ChartSource struct {
	Type    string          `json:"type"` // oci | https | git | tgz
	URL     string          `json:"url,omitempty"`
	RepoURL string          `json:"repoUrl,omitempty"`
	Chart   string          `json:"chart,omitempty"`
	Version string          `json:"version,omitempty"`
	Ref     string          `json:"ref,omitempty"`
	Path    string          `json:"path,omitempty"`
	TgzB64  string          `json:"tgzBase64,omitempty"`
	Auth    *Auth           `json:"auth,omitempty"`
}

type Auth struct {
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	SshKeyPem string `json:"sshKeyPem,omitempty"`
	Token     string `json:"token,omitempty"`
}

type PrereqSecret struct {
	Namespace  string            `json:"namespace"`
	Name       string            `json:"name"`
	Type       string            `json:"type,omitempty"`
	StringData map[string]string `json:"stringData"`
}

type InstallReq struct {
	Release         string                 `json:"release"`
	Namespace       string                 `json:"namespace"`
	Source          *ChartSource           `json:"source"`
	Values          map[string]any         `json:"values,omitempty"`
	Atomic          *bool                  `json:"atomic,omitempty"`
	Wait            *bool                  `json:"wait,omitempty"`
	TimeoutSeconds  int                    `json:"timeoutSeconds,omitempty"`
	CreateNamespace *bool                  `json:"createNamespace,omitempty"`
	Prerequisites   *struct {
		Secrets []PrereqSecret `json:"secrets"`
	} `json:"prerequisites,omitempty"`
}

type UpgradeReq struct {
	Source         *ChartSource   `json:"source"`
	Values         map[string]any `json:"values,omitempty"`
	Atomic         *bool          `json:"atomic,omitempty"`
	Wait           *bool          `json:"wait,omitempty"`
	TimeoutSeconds int            `json:"timeoutSeconds,omitempty"`
	ReuseValues    bool           `json:"reuseValues,omitempty"`
	ResetValues    bool           `json:"resetValues,omitempty"`
}

type RollbackReq struct {
	Revision       int  `json:"revision"`
	Wait           bool `json:"wait,omitempty"`
	TimeoutSeconds int  `json:"timeoutSeconds,omitempty"`
}

type DisableReq struct {
	Source            *ChartSource   `json:"source"`
	Values            map[string]any `json:"values,omitempty"`
	ResourceNamespace string         `json:"resourceNamespace,omitempty"`
	DeletePvcs        bool           `json:"deletePvcs,omitempty"`
	TimeoutSeconds    int            `json:"timeoutSeconds,omitempty"`
}

type PreflightReq struct {
	Release   string         `json:"release"`
	Namespace string         `json:"namespace"`
	Source    *ChartSource   `json:"source"`
	Values    map[string]any `json:"values,omitempty"`
}

type PreflightCheck struct {
	Name     string `json:"name"`
	Blocking bool   `json:"blocking"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message,omitempty"`
}

type PreflightResp struct {
	OK     bool             `json:"ok"`
	Checks []PreflightCheck `json:"checks"`
}

type ReleaseInfo struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   int    `json:"revision"`
	Status     string `json:"status"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type GetResp struct {
	Release    *ReleaseInfo                `json:"release"`
	Manifest   string                      `json:"manifest"`
	ValuesYAML string                      `json:"values_yaml"`
	Disabled   []DisabledResourceFromStore `json:"disabled"`
}

type DisabledResourceFromStore struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type HistoryEntry struct {
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	AppVersion  string `json:"app_version,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Description string `json:"description,omitempty"`
}

type DiffEntry struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Change    string `json:"change"`
	Detail    string `json:"detail,omitempty"`
}

type DiffResp struct {
	Drifted bool        `json:"drifted"`
	Entries []DiffEntry `json:"entries"`
}

// quick re-export so handlers don't need json import
var _ = json.Marshal

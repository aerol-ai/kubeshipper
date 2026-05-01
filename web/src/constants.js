export const NAV_ITEMS = [
	{ to: "/", label: "Overview", caption: "Cluster posture" },
	{ to: "/helm", label: "Helm", caption: "Release actions" },
	{ to: "/releases", label: "Releases", caption: "Diff and history" },
	{ to: "/services", label: "Services", caption: "Workload CRUD" },
	{ to: "/automations", label: "Auto Deploy", caption: "Digest watchers" },
];

export const PAGE_META = {
	"/": {
		title: "Control Deck",
		subtitle: "Watch releases, workloads, and rollout automation from a single operational surface.",
	},
	"/helm": {
		title: "Helm Flight Console",
		subtitle: "Install, upgrade, rollback, and remove releases with live streamed job output.",
	},
	"/releases": {
		title: "Release Inspector",
		subtitle: "Inspect revision history, manifest drift, values, and rendered output for a fuller ArgoCD-style workflow.",
	},
	"/services": {
		title: "Workload Composer",
		subtitle: "Create, patch, restart, and tail service deployments without leaving the dashboard.",
	},
	"/automations": {
		title: "Auto Deployment Watches",
		subtitle: "Track mutable image tags, pause automation, or force deployment on demand.",
	},
};

export const DEFAULT_SERVICE_SPEC = `{
  "name": "agent-gateway",
  "namespace": "default",
  "image": "ghcr.io/acme/agent-gateway:latest",
  "port": 8080,
  "replicas": 1,
  "public": false,
  "env": {
    "NODE_ENV": "production"
  }
}`;

export const INITIAL_HELM_FORM = {
	mode: "install",
	release: "",
	namespace: "default",
	sourceType: "oci",
	url: "",
	repoUrl: "",
	chart: "",
	version: "",
	ref: "",
	path: "",
	username: "",
	password: "",
	token: "",
	sshKeyPem: "",
	valuesText: "{}",
	timeoutSeconds: "600",
	atomic: true,
	wait: true,
	reuseValues: true,
	resetValues: false,
	rollbackRevision: "1",
	rolloutDeployment: "",
	rolloutService: "",
	rolloutContainer: "",
};
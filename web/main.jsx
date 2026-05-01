import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, NavLink, Navigate, Route, Routes, useLocation } from "react-router-dom";

const API_PREFIX = "/api";

const NAV_ITEMS = [
	{ to: "/", label: "Overview", caption: "Cluster posture" },
	{ to: "/helm", label: "Helm", caption: "Release control" },
	{ to: "/services", label: "Services", caption: "Workload CRUD" },
	{ to: "/automations", label: "Auto Deploy", caption: "Digest watchers" },
];

const PAGE_META = {
	"/": {
		title: "Control Deck",
		subtitle: "Watch releases, workloads, and rollout automation from a single operational surface.",
	},
	"/helm": {
		title: "Helm Flight Console",
		subtitle: "Install, upgrade, inspect, and retire releases with live streamed job output.",
	},
	"/services": {
		title: "Workload Composer",
		subtitle: "Create, patch, restart, and tail service deployments without leaving the dashboard.",
	},
	"/automations": {
		title: "Auto Deployment Watches",
		subtitle: "Track mutable image tags, pause automation, or force a rollout on demand.",
	},
};

const DEFAULT_SERVICE_SPEC = `{
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

const INITIAL_HELM_FORM = {
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

function AuthError(message) {
	const error = new Error(message);
	error.name = "AuthError";
	error.isAuth = true;
	return error;
}

function normalizeApiPath(input) {
	if (!input) {
		return API_PREFIX;
	}
	if (input.startsWith("http://") || input.startsWith("https://")) {
		return input;
	}
	if (input === API_PREFIX || input.startsWith(API_PREFIX + "/")) {
		return input;
	}
	if (input.startsWith("/")) {
		return API_PREFIX + input;
	}
	return API_PREFIX + "/" + input;
}

function parseJsonText(value, fallback) {
	const trimmed = value.trim();
	if (!trimmed) {
		return fallback;
	}
	return JSON.parse(trimmed);
}

function pretty(value) {
	return JSON.stringify(value ?? {}, null, 2);
}

function formatTime(value) {
	if (!value) {
		return "--";
	}
	const date = typeof value === "number" ? new Date(value) : new Date(value);
	if (Number.isNaN(date.getTime())) {
		return "--";
	}
	return new Intl.DateTimeFormat(undefined, {
		year: "numeric",
		month: "short",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	}).format(date);
}

function decodeError(error) {
	if (!error) {
		return "Unknown error";
	}
	if (typeof error === "string") {
		return error;
	}
	return error.message || "Unknown error";
}

function classNames(...values) {
	return values.filter(Boolean).join(" ");
}

function eventLine(event) {
	const prefix = event.phase ? `[${String(event.phase).toUpperCase()}]` : "[EVENT]";
	if (event.error) {
		return `${prefix} ${event.error}`;
	}
	if (event.message) {
		return `${prefix} ${event.message}`;
	}
	return `${prefix} ${JSON.stringify(event)}`;
}

function tryParseJson(text) {
	if (!text) {
		return null;
	}
	try {
		return JSON.parse(text);
	} catch {
		return null;
	}
}

function App() {
	const [session, setSession] = useState({ loading: true, authenticated: false, mode: "jwt", version: "" });
	const [banner, setBanner] = useState(null);
	const [consoleState, setConsoleState] = useState({
		title: "Live activity",
		subtitle: "Operation streams and workload logs show up here.",
		kind: "events",
		status: "idle",
		entries: ["No active operation."],
		content: "",
	});
	const streamRef = useRef(null);
	const bannerTimerRef = useRef(null);

	const stopActiveStream = () => {
		if (streamRef.current) {
			streamRef.current.stop();
			streamRef.current = null;
		}
	};

	const notify = (message, tone = "info") => {
		if (bannerTimerRef.current) {
			window.clearTimeout(bannerTimerRef.current);
		}
		setBanner({ message, tone });
		bannerTimerRef.current = window.setTimeout(() => setBanner(null), 4200);
	};

	const showSnapshot = (title, payload, status = "done", subtitle = "Latest response") => {
		stopActiveStream();
		const entries = Array.isArray(payload)
			? payload.map((line) => String(line))
			: [typeof payload === "string" ? payload : pretty(payload)];
		setConsoleState({
			title,
			subtitle,
			kind: "events",
			status,
			entries,
			content: "",
		});
	};

	const handleUnauthorized = () => {
		stopActiveStream();
		setSession((prev) => ({ ...prev, loading: false, authenticated: false, mode: "jwt" }));
		notify("Session expired. Sign in again.", "error");
	};

	const requestJson = async (path, options = {}) => {
		const response = await fetch(normalizeApiPath(path), {
			credentials: "same-origin",
			headers: {
				"Content-Type": "application/json",
				...(options.headers || {}),
			},
			...options,
		});
		const text = await response.text();
		const data = tryParseJson(text) ?? text;
		if (response.status === 401) {
			throw AuthError(typeof data === "object" && data?.error ? data.error : "Unauthorized");
		}
		if (!response.ok) {
			throw new Error(typeof data === "object" && data?.error ? data.error : text || `Request failed (${response.status})`);
		}
		return data;
	};

	const loadSession = async () => {
		try {
			const next = await requestJson("/auth/session", { method: "GET" });
			setSession({ loading: false, authenticated: !!next.authenticated, mode: next.mode || "jwt", version: next.version || "" });
		} catch (error) {
			if (error.isAuth) {
				setSession({ loading: false, authenticated: false, mode: "jwt", version: "" });
				return;
			}
			setSession({ loading: false, authenticated: false, mode: "jwt", version: "" });
			notify(`Session check failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		loadSession();
		return () => {
			stopActiveStream();
			if (bannerTimerRef.current) {
				window.clearTimeout(bannerTimerRef.current);
			}
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const login = async (token) => {
		const result = await requestJson("/auth/login", {
			method: "POST",
			body: JSON.stringify({ token }),
		});
		setSession({ loading: false, authenticated: !!result.authenticated, mode: result.mode || "jwt", version: result.version || session.version || "" });
		notify("Dashboard session established.", "success");
	};

	const logout = async () => {
		try {
			await requestJson("/auth/logout", { method: "POST" });
		} catch {
			// Ignore logout transport failures and still clear local state.
		}
		stopActiveStream();
		setSession((prev) => ({ ...prev, authenticated: false, mode: "jwt" }));
		notify("Session closed.", "info");
	};

	const startJobStream = (title, streamPath, onFinish) => {
		stopActiveStream();
		setConsoleState({
			title,
			subtitle: "Streaming server-side job events in real time.",
			kind: "events",
			status: "streaming",
			entries: ["Connecting to stream..."],
			content: "",
		});
		const source = new EventSource(normalizeApiPath(streamPath));
		let closed = false;
		const safeClose = () => {
			if (closed) {
				return;
			}
			closed = true;
			source.close();
		};
		source.onmessage = (message) => {
			const parsed = tryParseJson(message.data) ?? { message: message.data };
			setConsoleState((current) => ({
				...current,
				status: parsed.phase === "error" ? "error" : current.status,
				entries: [...current.entries.filter((entry) => entry !== "Connecting to stream..."), eventLine(parsed)],
			}));
			if (parsed.phase === "complete" || parsed.phase === "error") {
				safeClose();
				if (onFinish) {
					onFinish(parsed);
				}
			}
		};
		source.addEventListener("end", (message) => {
			const parsed = tryParseJson(message.data) ?? {};
			setConsoleState((current) => ({
				...current,
				status: parsed.status === "failed" ? "error" : "done",
				entries: [...current.entries, `[END] ${parsed.status || "completed"}`],
			}));
			safeClose();
			if (onFinish) {
				onFinish(parsed);
			}
		});
		source.onerror = () => {
			setConsoleState((current) => ({
				...current,
				status: current.status === "done" ? "done" : "error",
				entries: [...current.entries, "[STREAM] connection closed"],
			}));
			safeClose();
		};
		streamRef.current = { stop: safeClose };
	};

	const startTextStream = async (title, logPath) => {
		stopActiveStream();
		const controller = new AbortController();
		streamRef.current = { stop: () => controller.abort() };
		setConsoleState({
			title,
			subtitle: "Streaming workload logs.",
			kind: "text",
			status: "streaming",
			entries: [],
			content: "Connecting to logs...\n",
		});
		try {
			const response = await fetch(normalizeApiPath(logPath), {
				credentials: "same-origin",
				signal: controller.signal,
			});
			if (response.status === 401) {
				throw AuthError("Unauthorized");
			}
			if (!response.ok || !response.body) {
				const text = await response.text();
				throw new Error(text || `Failed to open log stream (${response.status})`);
			}
			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			for (;;) {
				const { value, done } = await reader.read();
				if (done) {
					break;
				}
				setConsoleState((current) => ({
					...current,
					content: current.content + decoder.decode(value, { stream: true }),
				}));
			}
			setConsoleState((current) => ({ ...current, status: "done" }));
		} catch (error) {
			if (error.isAuth) {
				handleUnauthorized();
				return;
			}
			if (error.name === "AbortError") {
				setConsoleState((current) => ({ ...current, status: current.status === "done" ? "done" : "idle" }));
				return;
			}
			setConsoleState((current) => ({
				...current,
				status: "error",
				content: current.content + `\n[error] ${decodeError(error)}\n`,
			}));
		}
	};

	if (session.loading) {
		return (
			<div className="login-shell">
				<section className="login-stage">
					<div>
						<span className="eyebrow">KubeShipper</span>
						<h1 className="login-heading">Loading the control deck.</h1>
						<p className="login-copy">Establishing session state and checking API reachability.</p>
					</div>
				</section>
			</div>
		);
	}

	return (
		<BrowserRouter>
			{banner ? <div className={classNames("banner", banner.tone)}>{banner.message}</div> : null}
			<Routes>
				<Route
					path="/login"
					element={
						session.authenticated || session.mode === "open" ? (
							<Navigate replace to="/" />
						) : (
							<LoginPage login={login} version={session.version} />
						)
					}
				/>
				<Route
					path="/*"
					element={
						session.authenticated || session.mode === "open" ? (
							<DashboardShell
								session={session}
								logout={logout}
								requestJson={requestJson}
								notify={notify}
								onUnauthorized={handleUnauthorized}
								consoleState={consoleState}
								showSnapshot={showSnapshot}
								startJobStream={startJobStream}
								startTextStream={startTextStream}
								refreshSession={loadSession}
								stopActiveStream={stopActiveStream}
							/>
						) : (
							<Navigate replace to="/login" />
						)
					}
				/>
			</Routes>
		</BrowserRouter>
	);
}

function LoginPage({ login, version }) {
	const [token, setToken] = useState("");
	const [error, setError] = useState("");
	const [busy, setBusy] = useState(false);

	const handleSubmit = async (event) => {
		event.preventDefault();
		setBusy(true);
		setError("");
		try {
			await login(token);
		} catch (err) {
			setError(decodeError(err));
		} finally {
			setBusy(false);
		}
	};

	return (
		<div className="login-shell">
			<section className="login-stage">
				<div>
					<span className="eyebrow">Shipper Aerol AI</span>
					<h1 className="login-heading">Mission control for every deployment path.</h1>
					<p className="login-copy">
						Operate Helm releases, hand-crafted services, and digest-based auto deployment watches from one argocd-style deck.
					</p>
				</div>
				<div className="feature-grid">
					<div className="feature-card">
						<p className="feature-title">Live operations</p>
						<p className="meta-copy">Stream Helm jobs, rollout events, and service logs without dropping back to curl.</p>
					</div>
					<div className="feature-card">
						<p className="feature-title">Automation control</p>
						<p className="meta-copy">Pause watchers, force syncs, and restart watched deployments from the same UI.</p>
					</div>
					<div className="feature-card">
						<p className="feature-title">Single token sign-in</p>
						<p className="meta-copy">Enter the existing API token once, receive a JWT-backed cookie session, and keep the browser authenticated.</p>
					</div>
					<div className="feature-card">
						<p className="feature-title">Version aware</p>
						<p className="meta-copy">The deck reads the running API version directly from the server and keeps the same /api surface the CLI uses.</p>
					</div>
				</div>
			</section>
			<section className="login-card">
				<span className="eyebrow">JWT Session</span>
				<h2 className="card-title" style={{ marginTop: 18 }}>Authenticate with the control key.</h2>
				<p className="card-subtitle">The token never lives in local storage. The server swaps it for a signed, HttpOnly session cookie.</p>
				<form className="login-form" onSubmit={handleSubmit}>
					<label className="label">
						Control key
						<input
							className="field"
							type="password"
							placeholder="Paste AUTH_TOKEN"
							value={token}
							onChange={(event) => setToken(event.target.value)}
							autoFocus
						/>
					</label>
					{error ? <div className="state-pill danger">{error}</div> : null}
					<div className="button-row">
						<button className="button" type="submit" disabled={busy || !token.trim()}>
							{busy ? "Authenticating..." : "Unlock dashboard"}
						</button>
					</div>
				</form>
				<p className="meta-copy" style={{ marginTop: 24 }}>
					Runtime version: <span className="mono">{version || "unknown"}</span>
				</p>
			</section>
		</div>
	);
}

function DashboardShell(props) {
	const location = useLocation();
	const meta = PAGE_META[location.pathname] || PAGE_META["/"];

	return (
		<div className="shell">
			<aside className="shell-sidebar">
				<div className="brand-mark">
					<span className="eyebrow">Argocd-grade basics</span>
					<h1 className="brand-title">KubeShipper</h1>
					<p className="brand-subtitle">React control deck running on the same binary that ships the cluster changes.</p>
				</div>
				<nav className="nav-list">
					{NAV_ITEMS.map((item) => (
						<NavLink
							key={item.to}
							to={item.to}
							end={item.to === "/"}
							className={({ isActive }) => classNames("nav-link", isActive && "active")}
						>
							<span className="nav-label">{item.label}</span>
							<span className="nav-caption">{item.caption}</span>
						</NavLink>
					))}
				</nav>
				<div className="sidebar-footer">
					<div className="button-row" style={{ justifyContent: "space-between", alignItems: "center" }}>
						<span className="session-pill">Auth mode: {props.session.mode}</span>
						<button className="ghost-button" type="button" onClick={props.logout}>
							Sign out
						</button>
					</div>
					<p className="meta-copy" style={{ marginTop: 14 }}>
						Version <span className="mono">{props.session.version || "dev"}</span>
					</p>
				</div>
			</aside>
			<main className="shell-main">
				<header className="topbar">
					<div>
						<span className="eyebrow">Operational surface</span>
						<h2 className="page-title">{meta.title}</h2>
						<p className="page-subtitle">{meta.subtitle}</p>
					</div>
					<div className="topbar-meta">
						<span className="state-pill success">Root app / API on /api</span>
						<button className="subtle-button" type="button" onClick={props.refreshSession}>
							Refresh session
						</button>
					</div>
				</header>
				<Routes>
					<Route path="/" element={<OverviewPage {...props} />} />
					<Route path="/helm" element={<HelmPage {...props} />} />
					<Route path="/services" element={<ServicesPage {...props} />} />
					<Route path="/automations" element={<AutomationsPage {...props} />} />
					<Route path="*" element={<Navigate replace to="/" />} />
				</Routes>
			</main>
			<aside className="shell-console">
				<div className="console-header">
					<div>
						<h3 className="console-title">{props.consoleState.title}</h3>
						<p className="console-subtitle">{props.consoleState.subtitle}</p>
					</div>
					<div className="button-row">
						<span className={classNames("state-pill", props.consoleState.status === "error" ? "danger" : props.consoleState.status === "streaming" ? "warning" : "success")}>
							{props.consoleState.status}
						</span>
						<button className="ghost-button" type="button" onClick={props.stopActiveStream}>
							Stop
						</button>
					</div>
				</div>
				{props.consoleState.kind === "text" ? (
					<pre className="console-content">{props.consoleState.content}</pre>
				) : (
					<div className="console-content console-list">
						{props.consoleState.entries.map((entry, index) => (
							<div key={`${entry}-${index}`} className={classNames("console-line", entry.toLowerCase().includes("error") && "error")}>
								{entry}
							</div>
						))}
					</div>
				)}
			</aside>
		</div>
	);
}

function OverviewPage({ requestJson, notify, onUnauthorized }) {
	const [state, setState] = useState({ loading: true, health: null, services: [], releases: [], watches: [] });

	const load = async () => {
		setState((current) => ({ ...current, loading: true }));
		try {
			const [health, servicesResponse, releasesResponse, watchesResponse] = await Promise.all([
				requestJson("/health"),
				requestJson("/services"),
				requestJson("/charts?all=true"),
				requestJson("/rollout-watches"),
			]);
			setState({
				loading: false,
				health,
				services: servicesResponse.services || [],
				releases: releasesResponse.releases || [],
				watches: watchesResponse.watches || [],
			});
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			setState((current) => ({ ...current, loading: false }));
			notify(`Overview refresh failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		load();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const enabledWatches = state.watches.filter((watch) => watch.enabled).length;

	return (
		<div className="view-grid">
			<section className="hero-card">
				<div>
					<span className="eyebrow">Realtime posture</span>
					<h3 className="hero-title">Run cluster changes with live feedback instead of terminal archaeology.</h3>
					<p className="hero-copy">
						This deck keeps the existing KubeShipper API intact while putting Helm operations, service mutations, rollout watches, and log tails in one operational UI.
					</p>
				</div>
				<div className="status-grid">
					<div className="mini-panel">
						<div className="mini-label">API health</div>
						<div className="mini-value">{state.health?.status || (state.loading ? "Loading" : "Unknown")}</div>
					</div>
					<div className="mini-panel">
						<div className="mini-label">Started</div>
						<div className="mini-value">{formatTime(state.health?.started_at)}</div>
					</div>
					<div className="mini-panel">
						<div className="mini-label">App version</div>
						<div className="mini-value mono">{state.health?.version || "dev"}</div>
					</div>
					<div className="mini-panel">
						<div className="mini-label">Enabled automations</div>
						<div className="mini-value">{enabledWatches}</div>
					</div>
				</div>
			</section>

			<section className="summary-grid">
				<SummaryCard label="Services" value={state.services.length} note="Service specs tracked in SQLite and mirrored to Kubernetes." />
				<SummaryCard label="Helm releases" value={state.releases.length} note="Live release inventory from the Helm SDK." />
				<SummaryCard label="Auto deployments" value={state.watches.length} note="Digest-based rollout watches registered for mutable tags." />
				<SummaryCard label="Pending posture" value={state.services.filter((service) => service.status === "PENDING").length} note="Workloads currently queued for reconciliation." />
			</section>

			<section className="split-grid">
				<div className="surface">
					<div className="card-header">
						<div>
							<h3 className="card-title">Service snapshot</h3>
							<p className="card-subtitle">The newest service records and their deployment states.</p>
						</div>
						<button className="subtle-button" type="button" onClick={load}>
							Refresh
						</button>
					</div>
					{state.services.length === 0 ? (
						<EmptyState title="No services yet" copy="Create your first service from the Services page to start streaming rollouts and pod logs." />
					) : (
						<ul className="metadata-list">
							{state.services.slice(0, 4).map((service) => (
								<li className="metadata-item" key={service.id}>
									<div className="metadata-top">
										<div>
											<div className="row-title">{service.id}</div>
											<div className="row-subtitle">Updated {formatTime(service.updated_at)}</div>
										</div>
										<StatusPill status={service.status} />
									</div>
								</li>
							))}
						</ul>
					)}
				</div>
				<div className="surface">
					<div className="card-header">
						<div>
							<h3 className="card-title">Automation posture</h3>
							<p className="card-subtitle">Recent rollout watches and their last recorded results.</p>
						</div>
					</div>
					{state.watches.length === 0 ? (
						<EmptyState title="No automation watches" copy="Register a watched deployment from the Auto Deploy page or attach a rolloutWatch block to a Helm install or upgrade." />
					) : (
						<ul className="metadata-list">
							{state.watches.slice(0, 4).map((watch) => (
								<li className="metadata-item" key={watch.id}>
									<div className="metadata-top">
										<div>
											<div className="row-title">{watch.namespace}/{watch.deployment}</div>
											<div className="row-subtitle">{watch.enabled ? "Enabled" : "Paused"} automation</div>
										</div>
										<span className={classNames("state-pill", watch.enabled ? "success" : "warning")}>{watch.last_result || (watch.enabled ? "ready" : "paused")}</span>
									</div>
									<div className="detail-meta">Latest digest: <span className="mono">{watch.latest_digest || "--"}</span></div>
								</li>
							))}
						</ul>
					)}
				</div>
			</section>
		</div>
	);
}

function SummaryCard({ label, value, note }) {
	return (
		<div className="summary-card">
			<div className="summary-label">{label}</div>
			<div className="summary-value">{value}</div>
			<div className="summary-footnote">{note}</div>
		</div>
	);
}

function HelmPage({ requestJson, notify, onUnauthorized, startJobStream, showSnapshot }) {
	const [form, setForm] = useState(INITIAL_HELM_FORM);
	const [releases, setReleases] = useState([]);
	const [details, setDetails] = useState(null);
	const [filters, setFilters] = useState({ namespace: "", all: true });
	const [busy, setBusy] = useState(false);

	const loadReleases = async () => {
		try {
			const params = new URLSearchParams();
			if (filters.namespace.trim()) {
				params.set("namespace", filters.namespace.trim());
			}
			params.set("all", filters.all ? "true" : "false");
			const response = await requestJson(`/charts?${params.toString()}`);
			setReleases(response.releases || []);
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Release list refresh failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		loadReleases();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [filters.namespace, filters.all]);

	const updateForm = (field, value) => setForm((current) => ({ ...current, [field]: value }));

	const submit = async (event) => {
		event.preventDefault();
		setBusy(true);
		try {
			const values = parseJsonText(form.valuesText || "{}", {});
			const rolloutWatch = buildRolloutWatch(form);
			if (form.mode === "install") {
				const response = await requestJson("/charts", {
					method: "POST",
					body: JSON.stringify({
						release: form.release.trim(),
						namespace: form.namespace.trim(),
						source: buildChartSource(form),
						values,
						atomic: form.atomic,
						wait: form.wait,
						timeoutSeconds: Number(form.timeoutSeconds) || 600,
						rolloutWatch,
					}),
				});
				startJobStream(`Install ${form.release.trim()}`, response.stream, loadReleases);
				notify(`Install job started for ${form.release.trim()}.`, "success");
			} else if (form.mode === "upgrade") {
				const response = await requestJson(`/charts/${encodeURIComponent(form.release.trim())}?namespace=${encodeURIComponent(form.namespace.trim())}`, {
					method: "PATCH",
					body: JSON.stringify({
						source: buildChartSource(form),
						values,
						atomic: form.atomic,
						wait: form.wait,
						timeoutSeconds: Number(form.timeoutSeconds) || 600,
						reuseValues: form.reuseValues,
						resetValues: form.resetValues,
						rolloutWatch,
					}),
				});
				startJobStream(`Upgrade ${form.release.trim()}`, response.stream, loadReleases);
				notify(`Upgrade job started for ${form.release.trim()}.`, "success");
			} else {
				const response = await requestJson(`/charts/${encodeURIComponent(form.release.trim())}/rollback?namespace=${encodeURIComponent(form.namespace.trim())}`, {
					method: "POST",
					body: JSON.stringify({
						revision: Number(form.rollbackRevision) || 1,
						wait: form.wait,
						timeoutSeconds: Number(form.timeoutSeconds) || 600,
					}),
				});
				showSnapshot(`Rollback ${form.release.trim()}`, response, "done", "Rollback completed without a background job.");
				notify(`Rolled back ${form.release.trim()} to revision ${form.rollbackRevision}.`, "success");
				loadReleases();
			}
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Helm action failed: ${decodeError(error)}`, "error");
		} finally {
			setBusy(false);
		}
	};

	const loadDetails = async (release) => {
		try {
			const namespace = release.namespace || form.namespace.trim();
			const [summary, history, values, manifest] = await Promise.all([
				requestJson(`/charts/${encodeURIComponent(release.name)}?namespace=${encodeURIComponent(namespace)}`),
				requestJson(`/charts/${encodeURIComponent(release.name)}/history?namespace=${encodeURIComponent(namespace)}`),
				requestJson(`/charts/${encodeURIComponent(release.name)}/values?namespace=${encodeURIComponent(namespace)}`),
				fetch(normalizeApiPath(`/charts/${encodeURIComponent(release.name)}/manifest?namespace=${encodeURIComponent(namespace)}`), { credentials: "same-origin" }).then((res) => res.text()),
			]);
			setDetails({ release, namespace, summary, history: history.entries || [], values: values.values_yaml || "", manifest });
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Release details failed: ${decodeError(error)}`, "error");
		}
	};

	const uninstallRelease = async (release) => {
		if (!window.confirm(`Uninstall ${release.name} from ${release.namespace}?`)) {
			return;
		}
		try {
			const response = await requestJson(`/charts/${encodeURIComponent(release.name)}?namespace=${encodeURIComponent(release.namespace)}&force=true`, {
				method: "DELETE",
			});
			showSnapshot(`Uninstall ${release.name}`, response, "done", "Uninstall completed synchronously.");
			notify(`Uninstalled ${release.name}.`, "success");
			loadReleases();
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Uninstall failed: ${decodeError(error)}`, "error");
		}
	};

	return (
		<div className="view-grid">
			<div className="split-grid">
				<section className="editor-card">
					<div className="card-header">
						<div>
							<h3 className="card-title">Release action form</h3>
							<p className="card-subtitle">Install, upgrade, or rollback with the same API contract the backend already exposes.</p>
						</div>
					</div>
					<form className="stack" onSubmit={submit}>
						<div className="segmented">
							{["install", "upgrade", "rollback"].map((mode) => (
								<button
									key={mode}
									className={classNames(form.mode === mode ? "button" : "ghost-button")}
									type="button"
									onClick={() => updateForm("mode", mode)}
								>
									{mode}
								</button>
							))}
						</div>
						<div className="field-grid">
							<label className="label">Release<input className="field" value={form.release} onChange={(event) => updateForm("release", event.target.value)} /></label>
							<label className="label">Namespace<input className="field" value={form.namespace} onChange={(event) => updateForm("namespace", event.target.value)} /></label>
						</div>
						{form.mode !== "rollback" ? (
							<>
								<div className="field-grid three">
									<label className="label">Source type
										<select className="select" value={form.sourceType} onChange={(event) => updateForm("sourceType", event.target.value)}>
											<option value="oci">OCI</option>
											<option value="https">HTTPS repo</option>
											<option value="git">Git</option>
											<option value="tgz">TGZ</option>
										</select>
									</label>
									<label className="label">Version<input className="field" value={form.version} onChange={(event) => updateForm("version", event.target.value)} /></label>
									<label className="label">Timeout seconds<input className="field" value={form.timeoutSeconds} onChange={(event) => updateForm("timeoutSeconds", event.target.value)} /></label>
								</div>
								{form.sourceType === "oci" ? <label className="label">OCI URL<input className="field" value={form.url} onChange={(event) => updateForm("url", event.target.value)} /></label> : null}
								{form.sourceType === "https" ? (
									<div className="field-grid">
										<label className="label">Repo URL<input className="field" value={form.repoUrl} onChange={(event) => updateForm("repoUrl", event.target.value)} /></label>
										<label className="label">Chart<input className="field" value={form.chart} onChange={(event) => updateForm("chart", event.target.value)} /></label>
									</div>
								) : null}
								{form.sourceType === "git" ? (
									<div className="field-grid">
										<label className="label">Repository URL<input className="field" value={form.repoUrl} onChange={(event) => updateForm("repoUrl", event.target.value)} /></label>
										<label className="label">Ref<input className="field" value={form.ref} onChange={(event) => updateForm("ref", event.target.value)} /></label>
									</div>
								) : null}
								{form.sourceType === "git" ? <label className="label">Chart path<input className="field" value={form.path} onChange={(event) => updateForm("path", event.target.value)} /></label> : null}
								{form.sourceType === "tgz" ? <label className="label">Base64 chart tarball<textarea className="textarea" value={form.url} onChange={(event) => updateForm("url", event.target.value)} /></label> : null}
								<div className="field-grid">
									<label className="label">Username<input className="field" value={form.username} onChange={(event) => updateForm("username", event.target.value)} /></label>
									<label className="label">Password<input className="field" type="password" value={form.password} onChange={(event) => updateForm("password", event.target.value)} /></label>
								</div>
								<div className="field-grid">
									<label className="label">Token<input className="field" type="password" value={form.token} onChange={(event) => updateForm("token", event.target.value)} /></label>
									<label className="label">SSH key PEM<textarea className="textarea" value={form.sshKeyPem} onChange={(event) => updateForm("sshKeyPem", event.target.value)} /></label>
								</div>
								<label className="label">Values JSON<textarea className="textarea" value={form.valuesText} onChange={(event) => updateForm("valuesText", event.target.value)} /></label>
								<div className="field-grid three">
									<label className="label">Rollout deployment<input className="field" value={form.rolloutDeployment} onChange={(event) => updateForm("rolloutDeployment", event.target.value)} /></label>
									<label className="label">Rollout service alias<input className="field" value={form.rolloutService} onChange={(event) => updateForm("rolloutService", event.target.value)} /></label>
									<label className="label">Container<input className="field" value={form.rolloutContainer} onChange={(event) => updateForm("rolloutContainer", event.target.value)} /></label>
								</div>
								<div className="checkbox-row">
									<label className="checkbox"><input type="checkbox" checked={form.atomic} onChange={(event) => updateForm("atomic", event.target.checked)} />Atomic</label>
									<label className="checkbox"><input type="checkbox" checked={form.wait} onChange={(event) => updateForm("wait", event.target.checked)} />Wait</label>
									<label className="checkbox"><input type="checkbox" checked={form.reuseValues} onChange={(event) => updateForm("reuseValues", event.target.checked)} />Reuse values</label>
									<label className="checkbox"><input type="checkbox" checked={form.resetValues} onChange={(event) => updateForm("resetValues", event.target.checked)} />Reset values</label>
								</div>
							</>
						) : (
							<div className="field-grid">
								<label className="label">Revision<input className="field" value={form.rollbackRevision} onChange={(event) => updateForm("rollbackRevision", event.target.value)} /></label>
								<label className="label">Timeout seconds<input className="field" value={form.timeoutSeconds} onChange={(event) => updateForm("timeoutSeconds", event.target.value)} /></label>
							</div>
						)}
						<div className="button-row">
							<button className="button" type="submit" disabled={busy}>
								{busy ? "Submitting..." : `Run ${form.mode}`}
							</button>
						</div>
					</form>
				</section>
				<section className="stack">
					<div className="table-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Release inventory</h3>
								<p className="card-subtitle">Live data from the Helm SDK.</p>
							</div>
							<div className="toolbar">
								<label className="checkbox"><input type="checkbox" checked={filters.all} onChange={(event) => setFilters((current) => ({ ...current, all: event.target.checked }))} />All namespaces</label>
								<input className="field" placeholder="Namespace filter" value={filters.namespace} onChange={(event) => setFilters((current) => ({ ...current, namespace: event.target.value }))} />
								<button className="ghost-button" type="button" onClick={loadReleases}>Refresh</button>
							</div>
						</div>
						{releases.length === 0 ? (
							<EmptyState title="No releases found" copy="Install your first chart from the form on the left or widen the namespace filter." />
						) : (
							<div className="table-wrap">
								<table className="table">
									<thead>
										<tr>
											<th>Release</th>
											<th>Status</th>
											<th>Revision</th>
											<th>Chart</th>
											<th>Actions</th>
										</tr>
									</thead>
									<tbody>
										{releases.map((release) => (
											<tr key={`${release.namespace}-${release.name}`}>
												<td>
													<span className="row-title">{release.name}</span>
													<span className="row-subtitle">{release.namespace}</span>
												</td>
												<td><StatusPill status={release.status} /></td>
												<td>{release.revision}</td>
												<td>{release.chart}</td>
												<td>
													<div className="inline-actions">
														<button className="ghost-button" type="button" onClick={() => loadDetails(release)}>Details</button>
														<button className="danger-button" type="button" onClick={() => uninstallRelease(release)}>Uninstall</button>
													</div>
												</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
						)}
					</div>
					<div className="detail-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Release detail</h3>
								<p className="card-subtitle">Manifest, values, and history pulled on demand.</p>
							</div>
						</div>
						{!details ? (
							<EmptyState title="No release selected" copy="Pick a release from the table to inspect its rendered manifest and history." />
						) : (
							<div className="stack">
								<div className="mini-panel">
									<div className="row-title">{details.release.name}</div>
									<div className="row-subtitle">Namespace: {details.namespace}</div>
								</div>
								<label className="label">Values YAML<textarea className="textarea" readOnly value={details.values} /></label>
								<label className="label">Manifest<textarea className="textarea" readOnly value={details.manifest} /></label>
								<div>
									<h4 className="card-title" style={{ fontSize: "1rem", marginBottom: 12 }}>History</h4>
									<ul className="timeline-list">
										{details.history.map((entry) => (
											<li className="timeline-item" key={`${entry.revision}-${entry.updated_at}`}>
												<div className="timeline-top">
													<strong>Revision {entry.revision}</strong>
													<span className="timeline-time">{formatTime(entry.updated_at)}</span>
												</div>
												<div className="detail-meta">{entry.status} · {entry.chart}</div>
											</li>
										))}
									</ul>
								</div>
							</div>
						)}
					</div>
				</section>
			</div>
		</div>
	);
}

function buildChartSource(form) {
	const auth = {};
	if (form.username.trim()) auth.username = form.username.trim();
	if (form.password.trim()) auth.password = form.password.trim();
	if (form.token.trim()) auth.token = form.token.trim();
	if (form.sshKeyPem.trim()) auth.sshKeyPem = form.sshKeyPem.trim();
	const source = { type: form.sourceType };
	if (form.sourceType === "oci") {
		source.url = form.url.trim();
		source.version = form.version.trim();
	} else if (form.sourceType === "https") {
		source.repoUrl = form.repoUrl.trim();
		source.chart = form.chart.trim();
		source.version = form.version.trim();
	} else if (form.sourceType === "git") {
		source.repoUrl = form.repoUrl.trim();
		source.ref = form.ref.trim();
		source.path = form.path.trim();
	} else if (form.sourceType === "tgz") {
		source.tgzBase64 = form.url.trim();
	}
	if (Object.keys(auth).length > 0) {
		source.auth = auth;
	}
	return source;
}

function buildRolloutWatch(form) {
	const payload = {};
	if (form.rolloutDeployment.trim()) payload.deployment = form.rolloutDeployment.trim();
	if (form.rolloutService.trim()) payload.service = form.rolloutService.trim();
	if (form.rolloutContainer.trim()) payload.container = form.rolloutContainer.trim();
	return Object.keys(payload).length > 0 ? payload : undefined;
}

function ServicesPage({ requestJson, notify, onUnauthorized, startJobStream, startTextStream }) {
	const [services, setServices] = useState([]);
	const [selected, setSelected] = useState(null);
	const [editor, setEditor] = useState(DEFAULT_SERVICE_SPEC);
	const [mode, setMode] = useState("create");
	const [busy, setBusy] = useState(false);

	const loadServices = async () => {
		try {
			const response = await requestJson("/services");
			setServices(response.services || []);
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Service list refresh failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		loadServices();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const selectService = async (service) => {
		setSelected(null);
		try {
			const detail = await requestJson(`/services/${encodeURIComponent(service.id)}`);
			setSelected(detail);
			setEditor(pretty(detail.spec));
			setMode("patch");
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Service detail failed: ${decodeError(error)}`, "error");
		}
	};

	const resetEditor = () => {
		setMode("create");
		setSelected(null);
		setEditor(DEFAULT_SERVICE_SPEC);
	};

	const submit = async (event) => {
		event.preventDefault();
		setBusy(true);
		try {
			const payload = parseJsonText(editor, {});
			if (mode === "create") {
				const response = await requestJson("/services", { method: "POST", body: JSON.stringify(payload) });
				startJobStream(`Create service ${payload.name}`, response.stream, loadServices);
				notify(`Service create started for ${payload.name}.`, "success");
			} else if (selected) {
				const response = await requestJson(`/services/${encodeURIComponent(selected.id)}`, { method: "PATCH", body: JSON.stringify(payload) });
				startJobStream(`Patch service ${selected.id}`, response.stream, () => {
					loadServices();
					selectService({ id: selected.id });
				});
				notify(`Service patch started for ${selected.id}.`, "success");
			}
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Service action failed: ${decodeError(error)}`, "error");
		} finally {
			setBusy(false);
		}
	};

	const deleteService = async (service) => {
		if (!window.confirm(`Delete ${service.id}?`)) {
			return;
		}
		try {
			const response = await requestJson(`/services/${encodeURIComponent(service.id)}?force=true`, { method: "DELETE" });
			startJobStream(`Delete service ${service.id}`, response.stream, loadServices);
			notify(`Delete started for ${service.id}.`, "success");
			if (selected?.id === service.id) {
				resetEditor();
			}
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Delete failed: ${decodeError(error)}`, "error");
		}
	};

	const restartService = async (service) => {
		try {
			const response = await requestJson(`/services/${encodeURIComponent(service.id)}/restart`, { method: "POST" });
			startJobStream(`Restart service ${service.id}`, response.stream, loadServices);
			notify(`Restart started for ${service.id}.`, "success");
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Restart failed: ${decodeError(error)}`, "error");
		}
	};

	return (
		<div className="view-grid">
			<div className="split-grid">
				<section className="editor-card">
					<div className="card-header">
						<div>
							<h3 className="card-title">Service spec editor</h3>
							<p className="card-subtitle">Use raw JSON for fast coverage of the full /services contract.</p>
						</div>
						<div className="button-row">
							<button className="ghost-button" type="button" onClick={resetEditor}>New spec</button>
						</div>
					</div>
					<form className="stack" onSubmit={submit}>
						<label className="label">Service JSON<textarea className="textarea" value={editor} onChange={(event) => setEditor(event.target.value)} /></label>
						<div className="button-row">
							<button className="button" type="submit" disabled={busy}>{busy ? "Submitting..." : mode === "create" ? "Create service" : `Patch ${selected?.id || "service"}`}</button>
						</div>
					</form>
				</section>
				<section className="stack">
					<div className="table-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Services</h3>
								<p className="card-subtitle">Current service specs and orchestration state.</p>
							</div>
							<button className="ghost-button" type="button" onClick={loadServices}>Refresh</button>
						</div>
						{services.length === 0 ? (
							<EmptyState title="No services tracked" copy="Create a service from the editor to start issuing rollout jobs and streaming pod logs." />
						) : (
							<div className="table-wrap">
								<table className="table">
									<thead>
										<tr>
											<th>Service</th>
											<th>Status</th>
											<th>Updated</th>
											<th>Actions</th>
										</tr>
									</thead>
									<tbody>
										{services.map((service) => (
											<tr key={service.id}>
												<td>
													<span className="row-title">{service.id}</span>
													<span className="row-subtitle">Created {formatTime(service.created_at)}</span>
												</td>
												<td><StatusPill status={service.status} /></td>
												<td>{formatTime(service.updated_at)}</td>
												<td>
													<div className="inline-actions">
														<button className="ghost-button" type="button" onClick={() => selectService(service)}>Inspect</button>
														<button className="subtle-button" type="button" onClick={() => restartService(service)}>Restart</button>
														<button className="ghost-button" type="button" onClick={() => startTextStream(`Logs for ${service.id}`, `/services/${encodeURIComponent(service.id)}/logs`)}>Logs</button>
														<button className="danger-button" type="button" onClick={() => deleteService(service)}>Delete</button>
													</div>
												</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
						)}
					</div>
					<div className="detail-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Selected service</h3>
								<p className="card-subtitle">Live Kubernetes status for the currently inspected service.</p>
							</div>
						</div>
						{!selected ? (
							<EmptyState title="No service selected" copy="Inspect a service from the table to load its live status and seed the JSON editor for patching." />
						) : (
							<div className="stack">
								<div className="mini-panel">
									<div className="row-title">{selected.id}</div>
									<div className="row-subtitle">{selected.k8sStatus?.reason || "Deployment tracked in cluster"}</div>
								</div>
								<ul className="metadata-list">
									<li className="metadata-item"><div className="metadata-top"><strong>Ready replicas</strong><span>{selected.k8sStatus?.readyReplicas ?? 0}</span></div></li>
									<li className="metadata-item"><div className="metadata-top"><strong>Total replicas</strong><span>{selected.k8sStatus?.totalReplicas ?? 0}</span></div></li>
								</ul>
								<label className="label">Resolved spec<textarea className="textarea" readOnly value={pretty(selected.spec)} /></label>
							</div>
						)}
					</div>
				</section>
			</div>
		</div>
	);
}

function AutomationsPage({ requestJson, notify, onUnauthorized, showSnapshot }) {
	const [watches, setWatches] = useState([]);
	const [selectedId, setSelectedId] = useState("");
	const [form, setForm] = useState({ namespace: "default", deployment: "", service: "", container: "" });

	const load = async () => {
		try {
			const response = await requestJson("/rollout-watches");
			const next = response.watches || [];
			setWatches(next);
			if (!selectedId && next[0]) {
				setSelectedId(next[0].id);
			}
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Automation refresh failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		load();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const selected = useMemo(() => watches.find((watch) => watch.id === selectedId) || null, [watches, selectedId]);

	const submit = async (event) => {
		event.preventDefault();
		try {
			const response = await requestJson("/rollout-watches", {
				method: "POST",
				body: JSON.stringify(form),
			});
			showSnapshot("Register rollout watch", response, "done", "Watch registration completed.");
			notify("Rollout watch saved.", "success");
			setSelectedId(response.watch.id);
			load();
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`Automation action failed: ${decodeError(error)}`, "error");
		}
	};

	const runAction = async (title, path, options) => {
		try {
			const response = await requestJson(path, options);
			showSnapshot(title, response, "done", "Automation action completed.");
			notify(title, "success");
			load();
		} catch (error) {
			if (error.isAuth) {
				onUnauthorized();
				return;
			}
			notify(`${title} failed: ${decodeError(error)}`, "error");
		}
	};

	return (
		<div className="view-grid">
			<div className="split-grid">
				<section className="editor-card">
					<div className="card-header">
						<div>
							<h3 className="card-title">Watch registration</h3>
							<p className="card-subtitle">Register deployments that should be rechecked every minute for mutable-tag digest drift.</p>
						</div>
					</div>
					<form className="stack" onSubmit={submit}>
						<div className="field-grid">
							<label className="label">Namespace<input className="field" value={form.namespace} onChange={(event) => setForm((current) => ({ ...current, namespace: event.target.value }))} /></label>
							<label className="label">Container<input className="field" value={form.container} onChange={(event) => setForm((current) => ({ ...current, container: event.target.value }))} /></label>
						</div>
						<div className="field-grid">
							<label className="label">Deployment<input className="field" value={form.deployment} onChange={(event) => setForm((current) => ({ ...current, deployment: event.target.value }))} /></label>
							<label className="label">Service alias<input className="field" value={form.service} onChange={(event) => setForm((current) => ({ ...current, service: event.target.value }))} /></label>
						</div>
						<div className="button-row"><button className="button" type="submit">Save watch</button></div>
					</form>
				</section>
				<section className="stack">
					<div className="table-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Watched deployments</h3>
								<p className="card-subtitle">Enable, pause, sync, restart, or remove auto deployment watches.</p>
							</div>
							<button className="ghost-button" type="button" onClick={load}>Refresh</button>
						</div>
						{watches.length === 0 ? (
							<EmptyState title="No watches registered" copy="Create a watch here or include rolloutWatch in your next Helm install or upgrade payload." />
						) : (
							<div className="table-wrap">
								<table className="table">
									<thead>
										<tr>
											<th>Target</th>
											<th>State</th>
											<th>Latest digest</th>
											<th>Actions</th>
										</tr>
									</thead>
									<tbody>
										{watches.map((watch) => (
											<tr key={watch.id} onClick={() => setSelectedId(watch.id)}>
												<td>
													<span className="row-title">{watch.namespace}/{watch.deployment}</span>
													<span className="row-subtitle">{watch.container || "single container"}</span>
												</td>
												<td>
													<div className="inline-actions">
														<span className={classNames("state-pill", watch.enabled ? "success" : "warning")}>{watch.enabled ? "enabled" : "paused"}</span>
														<span className="tag-pill">{watch.last_result || "registered"}</span>
													</div>
												</td>
												<td><span className="mono">{watch.latest_digest || "--"}</span></td>
												<td>
													<div className="inline-actions">
														<button className="ghost-button" type="button" onClick={(event) => { event.stopPropagation(); runAction(`Sync ${watch.deployment}`, `/rollout-watches/${watch.id}/sync`, { method: "POST" }); }}>Sync latest</button>
														<button className="subtle-button" type="button" onClick={(event) => { event.stopPropagation(); runAction(`Restart ${watch.deployment}`, `/rollout-watches/${watch.id}/restart`, { method: "POST" }); }}>Force rollout</button>
														<button className="ghost-button" type="button" onClick={(event) => { event.stopPropagation(); runAction(`${watch.enabled ? "Disable" : "Enable"} ${watch.deployment}`, `/rollout-watches/${watch.id}/${watch.enabled ? "disable" : "enable"}`, { method: "POST" }); }}>{watch.enabled ? "Turn off" : "Turn on"}</button>
														<button className="danger-button" type="button" onClick={(event) => { event.stopPropagation(); if (window.confirm(`Delete watch ${watch.namespace}/${watch.deployment}?`)) { runAction(`Delete ${watch.deployment}`, `/rollout-watches/${watch.id}`, { method: "DELETE" }); } }}>Delete</button>
													</div>
												</td>
											</tr>
										))}
									</tbody>
								</table>
							</div>
						)}
					</div>
					<div className="detail-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Timeline</h3>
								<p className="card-subtitle">Recent events recorded for the selected automation watch.</p>
							</div>
						</div>
						{!selected ? (
							<EmptyState title="No watch selected" copy="Click a watch in the table to inspect its timeline and current digest state." />
						) : (
							<div className="stack">
								<div className="mini-panel">
									<div className="row-title">{selected.namespace}/{selected.deployment}</div>
									<div className="row-subtitle">Current image: <span className="mono">{selected.current_image || "--"}</span></div>
								</div>
								<ul className="timeline-list">
									{(selected.timeline || []).slice().reverse().map((entry, index) => (
										<li className="timeline-item" key={`${entry.ts}-${index}`}>
											<div className="timeline-top">
												<strong>{entry.type}</strong>
												<span className="timeline-time">{formatTime(entry.ts)}</span>
											</div>
											<div>{entry.message || entry.error || "No message"}</div>
											{entry.latest_digest ? <div className="detail-meta">Latest digest: <span className="mono">{entry.latest_digest}</span></div> : null}
										</li>
									))}
								</ul>
							</div>
						)}
					</div>
				</section>
			</div>
		</div>
	);
}

function StatusPill({ status }) {
	const normalized = String(status || "unknown").toLowerCase();
	const tone = normalized.includes("fail") || normalized.includes("error") ? "danger" : normalized.includes("pending") || normalized.includes("deploy") ? "warning" : "success";
	return <span className={classNames("state-pill", tone)}>{status || "unknown"}</span>;
}

function EmptyState({ title, copy }) {
	return (
		<div className="empty-state">
			<h4 className="empty-title">{title}</h4>
			<p className="empty-copy">{copy}</p>
		</div>
	);
}

createRoot(document.getElementById("root")).render(<App />);
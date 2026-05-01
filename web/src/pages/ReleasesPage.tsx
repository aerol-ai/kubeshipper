import React, { startTransition, useDeferredValue, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { EmptyState } from "../components/EmptyState";
import { StatusPill } from "../components/StatusPill";
import { isAuthError } from "../lib/api";
import { classNames, decodeError, formatTime } from "../lib/format";
import { fetchReleaseBundle } from "../lib/releases";

type ReleaseSourceFormState = {
	url: string;
	version: string;
	monitorEnabled: boolean;
	username: string;
	password: string;
	token: string;
};

function sourceFormFromBundle(bundle) {
	if (!bundle) {
		return { url: "", version: "", monitorEnabled: false, username: "", password: "", token: "" };
	}
	const version = bundle.release?.chart_version || bundle.monitor?.current_version || "";
	if (bundle.source?.type === "oci") {
		return {
			url: bundle.source.url || "",
			version: bundle.source.version || version,
			monitorEnabled: Boolean(bundle.monitor?.monitor_enabled),
			username: "",
			password: "",
			token: "",
		};
	}
	return {
		url: "",
		version,
		monitorEnabled: Boolean(bundle.monitor?.monitor_enabled),
		username: "",
		password: "",
		token: "",
	};
}

function monitorTone(bundle) {
	if (!bundle?.monitor) {
		return "warning";
	}
	if (bundle.monitor.last_error) {
		return "danger";
	}
	const currentVersion = bundle.release?.chart_version || bundle.monitor.current_version || "";
	if (bundle.monitor.latest_version && currentVersion && bundle.monitor.latest_version !== currentVersion) {
		return "warning";
	}
	return bundle.monitor.monitor_enabled ? "success" : "warning";
}

function monitorLabel(bundle) {
	if (!bundle?.monitor) {
		return "not tracked";
	}
	if (bundle.monitor.last_error) {
		return "needs attention";
	}
	const currentVersion = bundle.release?.chart_version || bundle.monitor.current_version || "";
	if (bundle.monitor.latest_version && currentVersion && bundle.monitor.latest_version !== currentVersion) {
		return "update available";
	}
	return bundle.monitor.monitor_enabled ? "monitor on" : "monitor off";
}

export function ReleasesPage({ requestJson, requestText, notify, onUnauthorized, startJobStream, showSnapshot }) {
	const [searchParams, setSearchParams] = useSearchParams();
	const [releases, setReleases] = useState([]);
	const [filters, setFilters] = useState(() => ({
		namespace: searchParams.get("namespace") || "",
		all: !searchParams.get("namespace"),
		search: searchParams.get("release") || "",
	}));
	const deferredSearch = useDeferredValue(filters.search);
	const [selectedKey, setSelectedKey] = useState("");
	const [detailState, setDetailState] = useState({ loading: false, bundle: null });
	const [sourceForm, setSourceForm] = useState<ReleaseSourceFormState>({
		url: "",
		version: "",
		monitorEnabled: false,
		username: "",
		password: "",
		token: "",
	});
	const [activeAction, setActiveAction] = useState("");

	const releaseParam = searchParams.get("release") || "";
	const namespaceParam = searchParams.get("namespace") || "";

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
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			notify(`Release list refresh failed: ${decodeError(error)}`, "error");
		}
	};

	const loadDetails = async (release, syncUrl = true) => {
		const nextKey = `${release.namespace}/${release.name}`;
		setSelectedKey(nextKey);
		if (syncUrl) {
			setSearchParams({ release: release.name, namespace: release.namespace });
		}
		setDetailState((current) => ({ ...current, loading: true }));
		try {
			const bundle = await fetchReleaseBundle(requestJson, requestText, release.name, release.namespace);
			startTransition(() => {
				setDetailState({ loading: false, bundle });
			});
		} catch (error) {
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			setDetailState({ loading: false, bundle: null });
			notify(`Release inspection failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		loadReleases();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [filters.namespace, filters.all]);

	useEffect(() => {
		if (!releaseParam || !namespaceParam) {
			return;
		}
		const nextKey = `${namespaceParam}/${releaseParam}`;
		if (selectedKey === nextKey && detailState.bundle) {
			return;
		}
		loadDetails({ name: releaseParam, namespace: namespaceParam }, false);
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [releaseParam, namespaceParam]);

	useEffect(() => {
		setSourceForm(sourceFormFromBundle(detailState.bundle));
	}, [
		detailState.bundle?.release?.name,
		detailState.bundle?.namespace,
		detailState.bundle?.release?.chart_version,
		detailState.bundle?.source?.type,
		detailState.bundle?.source?.url,
		detailState.bundle?.source?.version,
		detailState.bundle?.monitor?.monitor_enabled,
	]);

	const visibleReleases = releases.filter((release) => {
		const search = deferredSearch.trim().toLowerCase();
		if (!search) {
			return true;
		}
		return [release.name, release.namespace, release.chart, release.app_version].some((value) =>
			String(value || "").toLowerCase().includes(search),
		);
	});

	const bundle = detailState.bundle;
	const diffEntries = bundle?.diff?.entries || [];
	const monitor = bundle?.monitor || null;
	const source = bundle?.source || null;
	const sourceAuthConfigured = Boolean(source?.auth_configured || monitor?.auth_configured);
	const currentVersion = bundle?.release?.chart_version || monitor?.current_version || "";
	const latestVersion = monitor?.latest_version || "";
	const updateAvailable = Boolean(currentVersion && latestVersion && currentVersion !== latestVersion);
	const sourceIsOCI = source?.type === "oci";

	const runSourceSave = async (event) => {
		event.preventDefault();
		if (!bundle) {
			return;
		}
		setActiveAction("source");
		try {
			const auth: { username?: string; password?: string; token?: string } = {};
			if (sourceForm.username.trim()) auth.username = sourceForm.username.trim();
			if (sourceForm.password.trim()) auth.password = sourceForm.password.trim();
			if (sourceForm.token.trim()) auth.token = sourceForm.token.trim();

			const response = await requestJson(
				`/charts/${encodeURIComponent(bundle.release.name)}/source?namespace=${encodeURIComponent(bundle.namespace)}`,
				{
					method: "PUT",
					body: JSON.stringify({
						source: {
							type: "oci",
							url: sourceForm.url.trim(),
							version: sourceForm.version.trim(),
							...(Object.keys(auth).length > 0 ? { auth } : {}),
						},
						monitorEnabled: sourceForm.monitorEnabled,
					}),
				},
			);
			showSnapshot(`Save chart source ${bundle.release.name}`, response, "done", "OCI source metadata saved.");
			notify("Chart source saved.", "success");
			await loadDetails({ name: bundle.release.name, namespace: bundle.namespace }, false);
		} catch (error) {
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			notify(`Source save failed: ${decodeError(error)}`, "error");
		} finally {
			setActiveAction("");
		}
	};

	const runMonitorCheck = async () => {
		if (!bundle) {
			return;
		}
		setActiveAction("check");
		try {
			const response = await requestJson(
				`/charts/${encodeURIComponent(bundle.release.name)}/monitor/check?namespace=${encodeURIComponent(bundle.namespace)}`,
				{ method: "POST" },
			);
			showSnapshot(`Check chart ${bundle.release.name}`, response, "done", response.message || "Chart check completed.");
			notify(response.message || "Chart check completed.", "success");
			await loadDetails({ name: bundle.release.name, namespace: bundle.namespace }, false);
		} catch (error) {
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			notify(`Chart check failed: ${decodeError(error)}`, "error");
		} finally {
			setActiveAction("");
		}
	};

	const runMonitorSync = async () => {
		if (!bundle) {
			return;
		}
		setActiveAction("sync");
		try {
			const response = await requestJson(
				`/charts/${encodeURIComponent(bundle.release.name)}/monitor/sync?namespace=${encodeURIComponent(bundle.namespace)}`,
				{ method: "POST" },
			);
			startJobStream(`Sync chart ${bundle.release.name}`, response.stream, async () => {
				await loadReleases();
				await loadDetails({ name: bundle.release.name, namespace: bundle.namespace }, false);
			});
			notify(`Chart sync job started for ${bundle.release.name}.`, "success");
		} catch (error) {
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			notify(`Chart sync failed: ${decodeError(error)}`, "error");
		} finally {
			setActiveAction("");
		}
	};

	const toggleMonitor = async (enabled) => {
		if (!bundle) {
			return;
		}
		setActiveAction("toggle");
		try {
			const response = await requestJson(
				`/charts/${encodeURIComponent(bundle.release.name)}/monitor/${enabled ? "enable" : "disable"}?namespace=${encodeURIComponent(bundle.namespace)}`,
				{ method: "POST" },
			);
			showSnapshot(
				`${enabled ? "Enable" : "Disable"} chart monitor ${bundle.release.name}`,
				response,
				"done",
				response.message || "Chart monitor updated.",
			);
			notify(response.message || "Chart monitor updated.", "success");
			await loadDetails({ name: bundle.release.name, namespace: bundle.namespace }, false);
		} catch (error) {
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			notify(`Chart monitor update failed: ${decodeError(error)}`, "error");
		} finally {
			setActiveAction("");
		}
	};

	return (
		<div className="view-grid">
			<div className="split-grid">
				<section className="table-card">
					<div className="card-header">
						<div>
							<h3 className="card-title">Release explorer</h3>
							<p className="card-subtitle">Select a Helm release to inspect drift, revisions, source tracking, and OCI chart sync state.</p>
						</div>
						<div className="toolbar">
							<label className="checkbox"><input type="checkbox" checked={filters.all} onChange={(event) => setFilters((current) => ({ ...current, all: event.target.checked }))} />All namespaces</label>
							<input className="field" placeholder="Search release or chart" value={filters.search} onChange={(event) => setFilters((current) => ({ ...current, search: event.target.value }))} />
							<input className="field" placeholder="Namespace filter" value={filters.namespace} onChange={(event) => setFilters((current) => ({ ...current, namespace: event.target.value }))} />
							<button className="ghost-button" type="button" onClick={loadReleases}>Refresh</button>
						</div>
					</div>
					{visibleReleases.length === 0 ? (
						<EmptyState title="No releases match" copy="Adjust the search or namespace filters, or go to the Helm page to install the first release." />
					) : (
						<div className="table-wrap">
							<table className="table table-clickable">
								<thead>
									<tr>
										<th>Release</th>
										<th>Status</th>
										<th>Revision</th>
										<th>Chart</th>
									</tr>
								</thead>
								<tbody>
									{visibleReleases.map((release) => {
										const key = `${release.namespace}/${release.name}`;
										return (
											<tr key={key} className={selectedKey === key ? "row-selected" : ""} onClick={() => loadDetails(release)}>
												<td>
													<span className="row-title">{release.name}</span>
													<span className="row-subtitle">{release.namespace}</span>
												</td>
												<td><StatusPill status={release.status} /></td>
												<td>{release.revision}</td>
												<td>{release.chart}</td>
											</tr>
										);
									})}
								</tbody>
							</table>
						</div>
					)}
				</section>

				<section className="stack">
					<div className="detail-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Release detail</h3>
								<p className="card-subtitle">Revision history, manifest drift, stored chart source, and upgrade automation for the selected release.</p>
							</div>
							<div className="button-row">
								{bundle ? <button className="ghost-button" type="button" onClick={() => loadDetails({ name: bundle.release.name, namespace: bundle.namespace }, false)}>Refresh detail</button> : null}
							</div>
						</div>
						{detailState.loading ? (
							<div className="empty-state"><h4 className="empty-title">Loading release</h4><p className="empty-copy">Fetching history, diff, values, manifest, and monitor state.</p></div>
						) : !bundle ? (
							<EmptyState title="No release selected" copy="Choose a release from the table to inspect its drift state, stored source metadata, and revision timeline." />
						) : (
							<div className="stack">
								<div className="surface">
									<div className="card-header">
										<div>
											<h3 className="card-title">{bundle.release.name}</h3>
											<p className="card-subtitle">Namespace {bundle.namespace} · revision {bundle.release.revision} · updated {formatTime(bundle.release.updated_at)}</p>
										</div>
										<div className="inline-actions">
											<StatusPill status={bundle.release.status} />
											<span className={classNames("state-pill", bundle.diff?.drifted ? "warning" : "success")}>
												{bundle.diff?.drifted ? "drifted" : "in sync"}
											</span>
											<span className={classNames("state-pill", monitorTone(bundle))}>{monitorLabel(bundle)}</span>
										</div>
									</div>
									<div className="detail-grid">
										<div className="mini-panel">
											<div className="mini-label">Chart</div>
											<div className="mini-value">{bundle.release.chart_name || bundle.release.chart}</div>
											<div className="detail-meta">Current version {currentVersion || "--"}</div>
										</div>
										<div className="mini-panel">
											<div className="mini-label">Source</div>
											<div className="mini-value">{source?.type ? source.type.toUpperCase() : "Untracked"}</div>
											<div className="detail-meta">{source?.type === "oci" ? source.url : "Attach an OCI source to enable chart sync."}</div>
										</div>
										<div className="mini-panel">
											<div className="mini-label">Monitor</div>
											<div className="mini-value">{monitor?.monitor_enabled ? "Enabled" : "Disabled"}</div>
											<div className="detail-meta">Last checked {formatTime(monitor?.last_checked_at)}</div>
										</div>
									</div>
								</div>

								<div className="surface">
									<div className="card-header">
										<div>
											<h3 className="card-title">Chart source & monitor</h3>
											<p className="card-subtitle">Attach OCI source metadata once, then validate or sync the release against the latest chart tag in the registry.</p>
										</div>
										<div className="button-row">
											<button className="ghost-button" type="button" onClick={runMonitorCheck} disabled={!sourceIsOCI || activeAction === "check"}>
												{activeAction === "check" ? "Checking..." : "Check OCI"}
											</button>
											<button className="button" type="button" onClick={runMonitorSync} disabled={!sourceIsOCI || activeAction === "sync"}>
												{activeAction === "sync" ? "Starting..." : updateAvailable ? `Upgrade to ${latestVersion}` : "Sync latest"}
											</button>
											<button
												className="ghost-button"
												type="button"
												onClick={() => toggleMonitor(!monitor?.monitor_enabled)}
												disabled={!sourceIsOCI || activeAction === "toggle"}
											>
												{activeAction === "toggle" ? "Saving..." : monitor?.monitor_enabled ? "Turn off monitor" : "Turn on monitor"}
											</button>
										</div>
									</div>

									<div className="stack-tight">
										{source ? (
											<div className="mini-panel">
												<div className="metadata-top">
													<div>
														<div className="row-title">{source.type.toUpperCase()} source</div>
														<div className="row-subtitle">{source.type === "oci" ? source.url : source.repoUrl || source.chart || "Stored release source"}</div>
													</div>
													<span className={classNames("state-pill", sourceIsOCI ? "success" : "warning")}>
														{sourceIsOCI ? "oci ready" : "manual only"}
													</span>
												</div>
											<div className="detail-meta">
												Stored version {source.version || "--"} · latest observed {latestVersion || "--"} · last sync {formatTime(monitor?.last_synced_at)}
											</div>
											{sourceAuthConfigured ? (
												<div className="detail-meta" style={{ marginTop: 8 }}>
													Stored OCI credentials are configured for this release. Secrets stay server-side and are not echoed back into the browser form.
												</div>
											) : null}
											{monitor?.last_error ? <div className="state-pill danger" style={{ marginTop: 10 }}>{monitor.last_error}</div> : null}
											</div>
										) : (
											<EmptyState title="No source metadata stored" copy="Attach the release's OCI chart source once so this page can check for newer chart versions and sync them automatically." />
										)}

										<form className="stack-tight" onSubmit={runSourceSave}>
											<div className="field-grid">
												<label className="label">
													OCI URL
													<input className="field" placeholder="oci://ghcr.io/acme/platform-chart" value={sourceForm.url} onChange={(event) => setSourceForm((current) => ({ ...current, url: event.target.value }))} />
												</label>
												<label className="label">
													Tracked version
													<input className="field" value={sourceForm.version} onChange={(event) => setSourceForm((current) => ({ ...current, version: event.target.value }))} />
												</label>
											</div>
											<div className="field-grid three">
												<label className="label">
													Registry username
													<input className="field" placeholder="github-username" value={sourceForm.username} onChange={(event) => setSourceForm((current) => ({ ...current, username: event.target.value }))} />
												</label>
												<label className="label">
													Password
													<input className="field" type="password" value={sourceForm.password} onChange={(event) => setSourceForm((current) => ({ ...current, password: event.target.value }))} />
												</label>
												<label className="label">
													Token / PAT
													<input className="field" type="password" value={sourceForm.token} onChange={(event) => setSourceForm((current) => ({ ...current, token: event.target.value }))} />
												</label>
											</div>
											<div className="detail-meta">
												Leave credential fields blank to keep stored OCI credentials. For GHCR, use your GitHub username plus a PAT in either Password or Token.
											</div>
											<div className="checkbox-row">
												<label className="checkbox"><input type="checkbox" checked={sourceForm.monitorEnabled} onChange={(event) => setSourceForm((current) => ({ ...current, monitorEnabled: event.target.checked }))} />Enable background chart monitor</label>
											</div>
											<div className="button-row">
												<button className="subtle-button" type="submit" disabled={activeAction === "source" || !sourceForm.url.trim() || !sourceForm.version.trim()}>
													{activeAction === "source" ? "Saving..." : source ? "Update OCI source" : "Attach OCI source"}
												</button>
											</div>
										</form>
									</div>
								</div>

								<div className="surface-grid">
									<div className="surface">
										<div className="card-header">
											<div>
												<h3 className="card-title">Presence diff</h3>
												<p className="card-subtitle">Current Helm manifest compared with live cluster presence.</p>
											</div>
										</div>
										{diffEntries.length === 0 ? (
											<EmptyState title="No drift detected" copy="Every rendered resource from this release currently exists in the cluster." />
										) : (
											<ul className="timeline-list">
												{diffEntries.map((entry, index) => (
													<li className="timeline-item" key={`${entry.kind}-${entry.name}-${index}`}>
														<div className="timeline-top">
															<strong>{entry.kind}/{entry.name}</strong>
															<span className="state-pill warning">{entry.change}</span>
														</div>
														<div>{entry.detail || "No detail"}</div>
														{entry.namespace ? <div className="detail-meta">Namespace {entry.namespace}</div> : null}
													</li>
												))}
											</ul>
										)}
									</div>

									<div className="surface">
										<div className="card-header">
											<div>
												<h3 className="card-title">Revision history</h3>
												<p className="card-subtitle">Track what changed across previous revisions.</p>
											</div>
										</div>
										{bundle.history.length === 0 ? (
											<EmptyState title="No history available" copy="Helm did not return revision history for this release." />
										) : (
											<ul className="timeline-list">
												{bundle.history.map((entry) => (
													<li className="timeline-item" key={`${entry.revision}-${entry.updated_at}`}>
														<div className="timeline-top">
															<strong>Revision {entry.revision}</strong>
															<span className="timeline-time">{formatTime(entry.updated_at)}</span>
														</div>
														<div className="detail-meta">{entry.status} · {entry.chart}</div>
														{entry.description ? <div>{entry.description}</div> : null}
													</li>
												))}
											</ul>
										)}
									</div>
								</div>

								<div className="surface-grid">
									<div className="surface code-surface">
										<div className="card-header">
											<div>
												<h3 className="card-title">Values YAML</h3>
												<p className="card-subtitle">Stored release values returned by Helm.</p>
											</div>
										</div>
										<textarea className="textarea code-area" readOnly value={bundle.values} />
									</div>

									<div className="surface code-surface">
										<div className="card-header">
											<div>
												<h3 className="card-title">Rendered manifest</h3>
												<p className="card-subtitle">Rendered manifest currently associated with the selected release.</p>
											</div>
										</div>
										<textarea className="textarea code-area" readOnly value={bundle.manifest} />
									</div>
								</div>
							</div>
						)}
					</div>
				</section>
			</div>
		</div>
	);
}

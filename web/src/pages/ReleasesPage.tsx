import React, { startTransition, useDeferredValue, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { EmptyState } from "../components/EmptyState";
import { StatusPill } from "../components/StatusPill";
import { isAuthError } from "../lib/api";
import { classNames, decodeError, formatTime } from "../lib/format";
import { fetchReleaseBundle } from "../lib/releases";

export function ReleasesPage({ requestJson, requestText, notify, onUnauthorized }) {
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

	return (
		<div className="view-grid">
			<div className="split-grid">
				<section className="table-card">
					<div className="card-header">
						<div>
							<h3 className="card-title">Release explorer</h3>
							<p className="card-subtitle">Select a Helm release to inspect revision history, drift, values, and the rendered manifest.</p>
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
							<table className="table">
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
											<tr key={key} onClick={() => loadDetails(release)}>
												<td>
													<span className="row-title">{release.name}</span>
													<span className="row-subtitle">{release.namespace}</span>
													{selectedKey === key ? <span className="tag-pill" style={{ marginTop: 8 }}>selected</span> : null}
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
								<p className="card-subtitle">History, drift, values, and manifest for the selected release.</p>
							</div>
							<div className="button-row">
								{bundle ? <button className="ghost-button" type="button" onClick={() => loadDetails({ name: bundle.release.name, namespace: bundle.namespace }, false)}>Refresh detail</button> : null}
							</div>
						</div>
						{detailState.loading ? (
							<div className="empty-state"><h4 className="empty-title">Loading release</h4><p className="empty-copy">Fetching history, diff, values, and rendered manifests.</p></div>
						) : !bundle ? (
							<EmptyState title="No release selected" copy="Choose a release from the table to inspect its current drift state and revision timeline." />
						) : (
							<div className="stack">
								<div className="mini-panel">
									<div className="metadata-top">
										<div>
											<div className="row-title">{bundle.release.name}</div>
											<div className="row-subtitle">Namespace {bundle.namespace}</div>
										</div>
										<div className="inline-actions">
											<StatusPill status={bundle.release.status} />
											<span className={classNames("state-pill", bundle.diff?.drifted ? "warning" : "success")}>
												{bundle.diff?.drifted ? "drifted" : "in sync"}
											</span>
										</div>
									</div>
									<div className="detail-meta">Revision {bundle.release.revision} · {bundle.release.chart} · updated {formatTime(bundle.release.updated_at)}</div>
								</div>

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

								<label className="label">Values YAML<textarea className="textarea" readOnly value={bundle.values} /></label>
								<label className="label">Rendered manifest<textarea className="textarea" readOnly value={bundle.manifest} /></label>
							</div>
						)}
					</div>
				</section>
			</div>
		</div>
	);
}
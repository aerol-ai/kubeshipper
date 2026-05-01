import React, { useEffect, useState } from "react";

import { EmptyState } from "../components/EmptyState";
import { isAuthError } from "../lib/api";
import { classNames, decodeError, formatTime } from "../lib/format";

export function AutomationsPage({ requestJson, notify, onUnauthorized, showSnapshot }) {
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
			if (isAuthError(error)) {
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

	const selected = watches.find((watch) => watch.id === selectedId) || null;

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
			if (isAuthError(error)) {
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
			if (isAuthError(error)) {
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
import React, { useEffect, useState } from "react";

import { DEFAULT_SERVICE_SPEC } from "../constants";
import { EmptyState } from "../components/EmptyState";
import { StatusPill } from "../components/StatusPill";
import { isAuthError } from "../lib/api";
import { decodeError, formatTime, parseJsonText, pretty } from "../lib/format";

export function ServicesPage({ requestJson, notify, onUnauthorized, startJobStream, startTextStream }) {
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
			if (isAuthError(error)) {
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
			if (isAuthError(error)) {
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
				startJobStream(`Patch service ${selected.id}`, response.stream, async () => {
					await loadServices();
					selectService({ id: selected.id });
				});
				notify(`Service patch started for ${selected.id}.`, "success");
			}
		} catch (error) {
			if (isAuthError(error)) {
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
			if (isAuthError(error)) {
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
			if (isAuthError(error)) {
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
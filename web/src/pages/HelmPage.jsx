import React, { useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { INITIAL_HELM_FORM } from "../constants";
import { EmptyState } from "../components/EmptyState";
import { StatusPill } from "../components/StatusPill";
import { isAuthError } from "../lib/api";
import { buildChartSource, buildRolloutWatch } from "../lib/charts";
import { decodeError, parseJsonText } from "../lib/format";

export function HelmPage({ requestJson, notify, onUnauthorized, startJobStream, showSnapshot }) {
	const [form, setForm] = useState(INITIAL_HELM_FORM);
	const [releases, setReleases] = useState([]);
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
			if (isAuthError(error)) {
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
			if (isAuthError(error)) {
				onUnauthorized();
				return;
			}
			notify(`Helm action failed: ${decodeError(error)}`, "error");
		} finally {
			setBusy(false);
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
			if (isAuthError(error)) {
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
									className={form.mode === mode ? "button" : "ghost-button"}
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
									<>
										<div className="field-grid">
											<label className="label">Repository URL<input className="field" value={form.repoUrl} onChange={(event) => updateForm("repoUrl", event.target.value)} /></label>
											<label className="label">Ref<input className="field" value={form.ref} onChange={(event) => updateForm("ref", event.target.value)} /></label>
										</div>
										<label className="label">Chart path<input className="field" value={form.path} onChange={(event) => updateForm("path", event.target.value)} /></label>
									</>
								) : null}
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
							<button className="button" type="submit" disabled={busy}>{busy ? "Submitting..." : `Run ${form.mode}`}</button>
						</div>
					</form>
				</section>
				<section className="stack">
					<div className="table-card">
						<div className="card-header">
							<div>
								<h3 className="card-title">Release inventory</h3>
								<p className="card-subtitle">Live data from the Helm SDK, with deep inspection moved into the Releases page.</p>
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
														<Link className="ghost-button" to={`/releases?release=${encodeURIComponent(release.name)}&namespace=${encodeURIComponent(release.namespace)}`}>
															Inspect
														</Link>
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
								<h3 className="card-title">Inspection workflow</h3>
								<p className="card-subtitle">Use the Releases page for revision history, drift diff, values, and rendered manifests.</p>
							</div>
						</div>
						<div className="stack">
							<div className="mini-panel">
								<div className="row-title">Why split this out?</div>
								<div className="row-subtitle">Operational actions stay here. Long-form inspection now lives in a dedicated page, which keeps the Helm surface maintainable.</div>
							</div>
							<EmptyState title="Need diff or history?" copy="Click Inspect on any release to open its revision timeline, drift status, values YAML, and rendered manifest in the Release Inspector." />
						</div>
					</div>
				</section>
			</div>
		</div>
	);
}
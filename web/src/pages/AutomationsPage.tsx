import React, { useEffect, useState } from "react";

import { EmptyState } from "../components/EmptyState";
import { isAuthError } from "../lib/api";
import { classNames, decodeError, formatTime } from "../lib/format";

type RolloutTargetContainer = {
	name: string;
	image: string;
	tracked_image?: string;
};

type RolloutTarget = {
	namespace: string;
	deployment: string;
	service?: string;
	containers: RolloutTargetContainer[];
};

type RolloutTargetCatalog = {
	namespaces: string[];
	targets: RolloutTarget[];
};

type RolloutWatchForm = {
	namespace: string;
	deployment: string;
	service: string;
	container: string;
};

const EMPTY_TARGET_CATALOG: RolloutTargetCatalog = { namespaces: [], targets: [] };
const EMPTY_FORM: RolloutWatchForm = { namespace: "", deployment: "", service: "", container: "" };

function defaultServiceForTarget(target: RolloutTarget | null) {
	if (!target) {
		return "";
	}
	return target.service || target.deployment;
}

function defaultContainerForTarget(target: RolloutTarget | null) {
	if (!target || target.containers.length !== 1) {
		return "";
	}
	return target.containers[0].name;
}

export function AutomationsPage({ requestJson, notify, onUnauthorized, showSnapshot }) {
	const [watches, setWatches] = useState<any[]>([]);
	const [selectedId, setSelectedId] = useState("");
	const [form, setForm] = useState<RolloutWatchForm>(EMPTY_FORM);
	const [targetCatalog, setTargetCatalog] = useState<RolloutTargetCatalog>(EMPTY_TARGET_CATALOG);

	const load = async () => {
		try {
			const [response, catalog] = await Promise.all([
				requestJson("/rollout-watches"),
				requestJson("/rollout-watches/targets"),
			]);
			const next = (response?.watches || []) as any[];
			const nextCatalog = (catalog || EMPTY_TARGET_CATALOG) as RolloutTargetCatalog;
			setWatches(next);
			setTargetCatalog({
				namespaces: nextCatalog.namespaces || [],
				targets: nextCatalog.targets || [],
			});
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

	useEffect(() => {
		if (targetCatalog.namespaces.length === 0) {
			return;
		}

		setForm((current) => {
			const nextNamespace = targetCatalog.namespaces.includes(current.namespace)
				? current.namespace
				: targetCatalog.namespaces[0];
			const namespaceTargets = targetCatalog.targets.filter((target) => target.namespace === nextNamespace);
			const matchingTarget = namespaceTargets.find((target) => target.deployment === current.deployment) || namespaceTargets[0] || null;
			const nextDeployment = matchingTarget ? matchingTarget.deployment : "";
			const nextService = matchingTarget
				? (current.deployment === nextDeployment && current.service ? current.service : defaultServiceForTarget(matchingTarget))
				: "";
			const hasCurrentContainer = matchingTarget
				? matchingTarget.containers.some((container) => container.name === current.container)
				: false;
			const nextContainer = matchingTarget
				? (hasCurrentContainer ? current.container : defaultContainerForTarget(matchingTarget))
				: "";

			if (
				current.namespace === nextNamespace &&
				current.deployment === nextDeployment &&
				current.service === nextService &&
				current.container === nextContainer
			) {
				return current;
			}

			return {
				...current,
				namespace: nextNamespace,
				deployment: nextDeployment,
				service: nextService,
				container: nextContainer,
			};
		});
	}, [targetCatalog]);

	const selected = watches.find((watch) => watch.id === selectedId) || null;
	const namespaceOptions = targetCatalog.namespaces;
	const deploymentOptions = targetCatalog.targets.filter((target) => target.namespace === form.namespace);
	const selectedTarget = deploymentOptions.find((target) => target.deployment === form.deployment) || null;
	const containerOptions = selectedTarget?.containers || [];

	const handleNamespaceChange = (namespace: string) => {
		const namespaceTargets = targetCatalog.targets.filter((target) => target.namespace === namespace);
		const firstTarget = namespaceTargets[0] || null;
		setForm({
			namespace,
			deployment: firstTarget?.deployment || "",
			service: defaultServiceForTarget(firstTarget),
			container: defaultContainerForTarget(firstTarget),
		});
	};

	const handleDeploymentChange = (deployment: string) => {
		const target = deploymentOptions.find((item) => item.deployment === deployment) || null;
		setForm((current) => ({
			...current,
			deployment,
			service: defaultServiceForTarget(target),
			container: target && target.containers.some((container) => container.name === current.container)
				? current.container
				: defaultContainerForTarget(target),
		}));
	};

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
							<p className="card-subtitle">Select an existing deployment from a managed namespace and register a watch for mutable-tag digest drift.</p>
						</div>
					</div>
					<form className="stack" onSubmit={submit}>
						<div className="field-grid">
							<label className="label">
								Namespace
								<select className="select" value={form.namespace} onChange={(event) => handleNamespaceChange(event.target.value)} disabled={namespaceOptions.length === 0}>
									<option value="">{namespaceOptions.length === 0 ? "No managed namespaces" : "Select namespace"}</option>
									{namespaceOptions.map((namespace) => (
										<option key={namespace} value={namespace}>{namespace}</option>
									))}
								</select>
							</label>
							<label className="label">
								Container
								<select className="select" value={form.container} onChange={(event) => setForm((current) => ({ ...current, container: event.target.value }))} disabled={containerOptions.length === 0}>
									<option value="">{containerOptions.length <= 1 ? "Auto-select when available" : "Select container"}</option>
									{containerOptions.map((container) => (
										<option key={container.name} value={container.name}>{container.name}</option>
									))}
								</select>
							</label>
						</div>
						<div className="field-grid">
							<label className="label">
								Deployment
								<select className="select" value={form.deployment} onChange={(event) => handleDeploymentChange(event.target.value)} disabled={deploymentOptions.length === 0}>
									<option value="">{deploymentOptions.length === 0 ? "No deployments in namespace" : "Select deployment"}</option>
									{deploymentOptions.map((target) => (
										<option key={`${target.namespace}/${target.deployment}`} value={target.deployment}>{target.deployment}</option>
									))}
								</select>
							</label>
							<label className="label">Service alias<input className="field" value={form.service} onChange={(event) => setForm((current) => ({ ...current, service: event.target.value }))} /></label>
						</div>
						{selectedTarget ? (
							<div className="mini-panel">
								<div className="row-title">{selectedTarget.namespace}/{selectedTarget.deployment}</div>
								<div className="row-subtitle">
									{selectedTarget.containers.length === 0
										? "No containers discovered"
										: selectedTarget.containers.map((container) => `${container.name}: ${container.tracked_image || container.image}`).join(" | ")}
								</div>
							</div>
						) : (
							<EmptyState title="No deployment selected" copy="Choose a namespace to see existing deployments and autofill the rollout watch form." />
						)}
						<div className="button-row"><button className="button" type="submit" disabled={!form.namespace || !form.deployment}>Save watch</button></div>
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
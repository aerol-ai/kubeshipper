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

function watchTone(watch: any) {
	if (!watch.enabled) {
		return "warning";
	}
	if (watch.last_result === "error") {
		return "danger";
	}
	return "success";
}

function watchTimestamp(watch: any) {
	return watch.last_synced_at || watch.last_checked_at || watch.updated_at || watch.created_at;
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
			setSelectedId((current) => {
				if (current && next.some((watch) => watch.id === current)) {
					return current;
				}
				return next[0]?.id || "";
			});
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
		<div className="view-grid automation-layout">
			<section className="surface automation-register-card">
				<div className="card-header">
					<div>
						<h3 className="card-title">Watch registration</h3>
						<p className="card-subtitle">Pick a managed deployment, confirm the tracked container, then save the watch. This stays compact because it is a setup step, not the main workspace.</p>
					</div>
				</div>
				<form className="automation-register-form" onSubmit={submit}>
					<div className="automation-register-grid">
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
							Deployment
							<select className="select" value={form.deployment} onChange={(event) => handleDeploymentChange(event.target.value)} disabled={deploymentOptions.length === 0}>
								<option value="">{deploymentOptions.length === 0 ? "No deployments in namespace" : "Select deployment"}</option>
								{deploymentOptions.map((target) => (
									<option key={`${target.namespace}/${target.deployment}`} value={target.deployment}>{target.deployment}</option>
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
						<label className="label">
							Service alias
							<input className="field" value={form.service} onChange={(event) => setForm((current) => ({ ...current, service: event.target.value }))} />
						</label>
					</div>
					<div className="automation-register-footer">
						{selectedTarget ? (
							<div className="mini-panel automation-register-preview">
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
						<div className="automation-register-actions">
							<button className="button" type="submit" disabled={!form.namespace || !form.deployment}>Save watch</button>
						</div>
					</div>
				</form>
			</section>

			<section className="surface">
				<div className="card-header">
					<div>
						<h3 className="card-title">Watched deployments</h3>
						<p className="card-subtitle">Each watch is its own operational card. Status sits with the target, and actions stay underneath the content instead of getting shoved into a clipped table column.</p>
					</div>
					<button className="ghost-button" type="button" onClick={load}>Refresh</button>
				</div>
				{watches.length === 0 ? (
					<EmptyState title="No watches registered" copy="Create a watch here or include rolloutWatch in your next Helm install or upgrade payload." />
				) : (
					<div className="watch-list">
						{watches.map((watch) => (
							<article
								key={watch.id}
								className={classNames("watch-card", selectedId === watch.id && "selected")}
								onClick={() => setSelectedId(watch.id)}
							>
								<div className="watch-card-top">
									<div className="watch-card-heading">
										<div className="row-title">{watch.namespace}/{watch.deployment}</div>
										<div className="row-subtitle">{watch.container || "Single container watch"}</div>
									</div>
									<div className="watch-card-pills">
										<span className={classNames("state-pill", watchTone(watch))}>{watch.enabled ? "enabled" : "paused"}</span>
										<span className="tag-pill">{watch.last_result || "registered"}</span>
									</div>
								</div>
								<div className="watch-card-grid">
									<div className="watch-card-section">
										<div className="mini-label">Current image</div>
										<div className="watch-card-code">{watch.current_image || watch.tracked_image || "--"}</div>
									</div>
									<div className="watch-card-section">
										<div className="mini-label">Latest digest</div>
										<div className="watch-card-code">{watch.latest_digest || "--"}</div>
									</div>
									<div className="watch-card-section">
										<div className="mini-label">Checks / syncs</div>
										<div className="watch-card-value">{watch.check_count || 0} checks · {watch.sync_count || 0} syncs</div>
									</div>
									<div className="watch-card-section">
										<div className="mini-label">Last activity</div>
										<div className="watch-card-value">{formatTime(watchTimestamp(watch))}</div>
									</div>
								</div>
								<div className="watch-card-actions">
									<button className="ghost-button" type="button" onClick={(event) => { event.stopPropagation(); runAction(`Sync ${watch.deployment}`, `/rollout-watches/${watch.id}/sync`, { method: "POST" }); }}>Sync latest</button>
									<button className="subtle-button" type="button" onClick={(event) => { event.stopPropagation(); runAction(`Restart ${watch.deployment}`, `/rollout-watches/${watch.id}/restart`, { method: "POST" }); }}>Force rollout</button>
									<button className="ghost-button" type="button" onClick={(event) => { event.stopPropagation(); runAction(`${watch.enabled ? "Disable" : "Enable"} ${watch.deployment}`, `/rollout-watches/${watch.id}/${watch.enabled ? "disable" : "enable"}`, { method: "POST" }); }}>{watch.enabled ? "Turn off" : "Turn on"}</button>
									<button className="danger-button" type="button" onClick={(event) => { event.stopPropagation(); if (window.confirm(`Delete watch ${watch.namespace}/${watch.deployment}?`)) { runAction(`Delete ${watch.deployment}`, `/rollout-watches/${watch.id}`, { method: "DELETE" }); } }}>Delete</button>
								</div>
							</article>
						))}
					</div>
				)}
			</section>

			<section className="surface">
				<div className="card-header">
					<div>
						<h3 className="card-title">Timeline</h3>
						<p className="card-subtitle">Recent events for the selected watch, with the current image pinned above the log.</p>
					</div>
				</div>
				{!selected ? (
					<EmptyState title="No watch selected" copy="Select a watched deployment to inspect its timeline and current digest state." />
				) : (
					<div className="stack">
						<div className="detail-grid">
							<div className="mini-panel">
								<div className="mini-label">Selected target</div>
								<div className="mini-value">{selected.namespace}/{selected.deployment}</div>
								<div className="detail-meta">{selected.container || "Single container watch"}</div>
							</div>
							<div className="mini-panel">
								<div className="mini-label">Current image</div>
								<div className="watch-card-code">{selected.current_image || "--"}</div>
							</div>
							<div className="mini-panel">
								<div className="mini-label">Latest digest</div>
								<div className="watch-card-code">{selected.latest_digest || "--"}</div>
							</div>
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
			</section>
		</div>
	);
}

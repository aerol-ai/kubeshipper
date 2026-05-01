import React, { useEffect, useState } from "react";

import { EmptyState } from "../components/EmptyState";
import { StatusPill } from "../components/StatusPill";
import { SummaryCard } from "../components/SummaryCard";
import { isAuthError } from "../lib/api";
import { decodeError, formatTime, classNames } from "../lib/format";

export function OverviewPage({ requestJson, notify, onUnauthorized }) {
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
			if (isAuthError(error)) {
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
						This deck keeps the existing KubeShipper API intact while putting Helm operations, service mutations, rollout watches, release diff, and log tails in one operational UI.
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
						<button className="subtle-button" type="button" onClick={load}>Refresh</button>
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
										<span className={classNames("state-pill", watch.enabled ? "success" : "warning")}>
											{watch.last_result || (watch.enabled ? "ready" : "paused")}
										</span>
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
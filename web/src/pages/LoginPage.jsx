import React, { useState } from "react";

import { decodeError } from "../lib/format";

export function LoginPage({ login, version }) {
	const [token, setToken] = useState("");
	const [error, setError] = useState("");
	const [busy, setBusy] = useState(false);

	const handleSubmit = async (event) => {
		event.preventDefault();
		setBusy(true);
		setError("");
		try {
			await login(token);
		} catch (nextError) {
			setError(decodeError(nextError));
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
						<p className="meta-copy">Enter the API token once, receive a signed session cookie, and keep the browser authenticated.</p>
					</div>
					<div className="feature-card">
						<p className="feature-title">Release insight</p>
						<p className="meta-copy">Inspect release history and drift from the same deck instead of stitching together CLI calls.</p>
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
import React from "react";

export function LoadingScreen() {
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
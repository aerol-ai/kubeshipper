export function buildChartSource(form) {
	const auth = {};
	if (form.username.trim()) auth.username = form.username.trim();
	if (form.password.trim()) auth.password = form.password.trim();
	if (form.token.trim()) auth.token = form.token.trim();
	if (form.sshKeyPem.trim()) auth.sshKeyPem = form.sshKeyPem.trim();

	const source = { type: form.sourceType };
	if (form.sourceType === "oci") {
		source.url = form.url.trim();
		source.version = form.version.trim();
	} else if (form.sourceType === "https") {
		source.repoUrl = form.repoUrl.trim();
		source.chart = form.chart.trim();
		source.version = form.version.trim();
	} else if (form.sourceType === "git") {
		source.repoUrl = form.repoUrl.trim();
		source.ref = form.ref.trim();
		source.path = form.path.trim();
	} else if (form.sourceType === "tgz") {
		source.tgzBase64 = form.url.trim();
	}

	if (Object.keys(auth).length > 0) {
		source.auth = auth;
	}

	return source;
}

export function buildRolloutWatch(form) {
	const payload = {};
	if (form.rolloutDeployment.trim()) payload.deployment = form.rolloutDeployment.trim();
	if (form.rolloutService.trim()) payload.service = form.rolloutService.trim();
	if (form.rolloutContainer.trim()) payload.container = form.rolloutContainer.trim();
	return Object.keys(payload).length > 0 ? payload : undefined;
}
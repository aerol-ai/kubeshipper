export async function fetchReleaseBundle(requestJson, requestText, release, namespace) {
	const encodedRelease = encodeURIComponent(release);
	const encodedNamespace = encodeURIComponent(namespace);
	const [summary, history, diff, values, manifest] = await Promise.all([
		requestJson(`/charts/${encodedRelease}?namespace=${encodedNamespace}`),
		requestJson(`/charts/${encodedRelease}/history?namespace=${encodedNamespace}`),
		requestJson(`/charts/${encodedRelease}/diff?namespace=${encodedNamespace}`),
		requestJson(`/charts/${encodedRelease}/values?namespace=${encodedNamespace}`),
		requestText(`/charts/${encodedRelease}/manifest?namespace=${encodedNamespace}`),
	]);

	return {
		release: summary.release,
		namespace,
		summary,
		history: history.entries || [],
		diff,
		values: values.values_yaml || "",
		manifest,
	};
}
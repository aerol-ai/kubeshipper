const API_PREFIX = "/api";

export function AuthError(message) {
	const error = new Error(message);
	error.name = "AuthError";
	error.isAuth = true;
	return error;
}

export function isAuthError(error) {
	return Boolean(error?.isAuth);
}

export function normalizeApiPath(input) {
	if (!input) {
		return API_PREFIX;
	}
	if (input.startsWith("http://") || input.startsWith("https://")) {
		return input;
	}
	if (input === API_PREFIX || input.startsWith(API_PREFIX + "/")) {
		return input;
	}
	if (input.startsWith("/")) {
		return API_PREFIX + input;
	}
	return API_PREFIX + "/" + input;
}

export async function requestJson(path, options = {}) {
	const response = await fetch(normalizeApiPath(path), {
		credentials: "same-origin",
		headers: {
			"Content-Type": "application/json",
			...(options.headers || {}),
		},
		...options,
	});
	const text = await response.text();
	let data = text;
	if (text) {
		try {
			data = JSON.parse(text);
		} catch {
			data = text;
		}
	}
	if (response.status === 401) {
		throw AuthError(typeof data === "object" && data?.error ? data.error : "Unauthorized");
	}
	if (!response.ok) {
		throw new Error(typeof data === "object" && data?.error ? data.error : text || `Request failed (${response.status})`);
	}
	return data;
}

export async function requestText(path, options = {}) {
	const response = await fetch(normalizeApiPath(path), {
		credentials: "same-origin",
		...options,
	});
	const text = await response.text();
	if (response.status === 401) {
		throw AuthError(text || "Unauthorized");
	}
	if (!response.ok) {
		throw new Error(text || `Request failed (${response.status})`);
	}
	return text;
}
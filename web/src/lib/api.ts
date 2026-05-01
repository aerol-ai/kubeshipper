const API_PREFIX = "/api";

type AuthenticatedError = Error & { isAuth: true };
type ApiErrorShape = { error?: string };

export function AuthError(message: string): AuthenticatedError {
	const error = new Error(message) as AuthenticatedError;
	error.name = "AuthError";
	error.isAuth = true;
	return error;
}

export function isAuthError(error: unknown): error is AuthenticatedError {
	return Boolean(typeof error === "object" && error && "isAuth" in error);
}

export function normalizeApiPath(input: string) {
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

export async function requestJson<T = unknown>(path: string, options: RequestInit = {}): Promise<T> {
	const response = await fetch(normalizeApiPath(path), {
		credentials: "same-origin",
		headers: {
			"Content-Type": "application/json",
			...((options.headers || {}) as HeadersInit),
		},
		...options,
	});
	const text = await response.text();
	let data: unknown = text;
	if (text) {
		try {
			data = JSON.parse(text);
		} catch {
			data = text;
		}
	}
	const apiError = typeof data === "object" && data !== null ? (data as ApiErrorShape).error : undefined;
	if (response.status === 401) {
		throw AuthError(apiError || "Unauthorized");
	}
	if (!response.ok) {
		throw new Error(apiError || text || `Request failed (${response.status})`);
	}
	return data as T;
}

export async function requestText(path: string, options: RequestInit = {}): Promise<string> {
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
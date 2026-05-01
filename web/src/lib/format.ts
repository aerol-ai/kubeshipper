export function parseJsonText(value, fallback) {
	const trimmed = value.trim();
	if (!trimmed) {
		return fallback;
	}
	return JSON.parse(trimmed);
}

export function pretty(value) {
	return JSON.stringify(value ?? {}, null, 2);
}

export function formatTime(value) {
	if (!value) {
		return "--";
	}
	const date = typeof value === "number" ? new Date(value) : new Date(value);
	if (Number.isNaN(date.getTime())) {
		return "--";
	}
	return new Intl.DateTimeFormat(undefined, {
		year: "numeric",
		month: "short",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	}).format(date);
}

export function decodeError(error) {
	if (!error) {
		return "Unknown error";
	}
	if (typeof error === "string") {
		return error;
	}
	return error.message || "Unknown error";
}

export function classNames(...values) {
	return values.filter(Boolean).join(" ");
}

export function tryParseJson(text) {
	if (!text) {
		return null;
	}
	try {
		return JSON.parse(text);
	} catch {
		return null;
	}
}

export function eventLine(event) {
	const prefix = event.phase ? `[${String(event.phase).toUpperCase()}]` : "[EVENT]";
	if (event.error) {
		return `${prefix} ${event.error}`;
	}
	if (event.message) {
		return `${prefix} ${event.message}`;
	}
	return `${prefix} ${JSON.stringify(event)}`;
}
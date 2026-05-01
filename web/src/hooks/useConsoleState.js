import { useEffect, useRef, useState } from "react";

import { normalizeApiPath } from "../lib/api";
import { decodeError, eventLine, pretty, tryParseJson } from "../lib/format";

export function useConsoleState({ onUnauthorized }) {
	const [consoleState, setConsoleState] = useState({
		title: "Live activity",
		subtitle: "Operation streams and workload logs show up here.",
		kind: "events",
		status: "idle",
		entries: ["No active operation."],
		content: "",
	});
	const streamRef = useRef(null);

	const stopActiveStream = () => {
		if (streamRef.current) {
			streamRef.current.stop();
			streamRef.current = null;
		}
	};

	useEffect(() => () => stopActiveStream(), []);

	const showSnapshot = (title, payload, status = "done", subtitle = "Latest response") => {
		stopActiveStream();
		const entries = Array.isArray(payload)
			? payload.map((line) => String(line))
			: [typeof payload === "string" ? payload : pretty(payload)];
		setConsoleState({
			title,
			subtitle,
			kind: "events",
			status,
			entries,
			content: "",
		});
	};

	const startJobStream = (title, streamPath, onFinish) => {
		stopActiveStream();
		setConsoleState({
			title,
			subtitle: "Streaming server-side job events in real time.",
			kind: "events",
			status: "streaming",
			entries: ["Connecting to stream..."],
			content: "",
		});

		const source = new EventSource(normalizeApiPath(streamPath));
		let closed = false;
		const safeClose = () => {
			if (closed) {
				return;
			}
			closed = true;
			source.close();
		};

		source.onmessage = (message) => {
			const parsed = tryParseJson(message.data) ?? { message: message.data };
			setConsoleState((current) => ({
				...current,
				status: parsed.phase === "error" ? "error" : current.status,
				entries: [...current.entries.filter((entry) => entry !== "Connecting to stream..."), eventLine(parsed)],
			}));
			if (parsed.phase === "complete" || parsed.phase === "error") {
				safeClose();
				if (onFinish) {
					onFinish(parsed);
				}
			}
		};

		source.addEventListener("end", (message) => {
			const parsed = tryParseJson(message.data) ?? {};
			setConsoleState((current) => ({
				...current,
				status: parsed.status === "failed" ? "error" : "done",
				entries: [...current.entries, `[END] ${parsed.status || "completed"}`],
			}));
			safeClose();
			if (onFinish) {
				onFinish(parsed);
			}
		});

		source.onerror = () => {
			setConsoleState((current) => ({
				...current,
				status: current.status === "done" ? "done" : "error",
				entries: [...current.entries, "[STREAM] connection closed"],
			}));
			safeClose();
		};

		streamRef.current = { stop: safeClose };
	};

	const startTextStream = async (title, logPath) => {
		stopActiveStream();
		const controller = new AbortController();
		streamRef.current = { stop: () => controller.abort() };
		setConsoleState({
			title,
			subtitle: "Streaming workload logs.",
			kind: "text",
			status: "streaming",
			entries: [],
			content: "Connecting to logs...\n",
		});

		try {
			const response = await fetch(normalizeApiPath(logPath), {
				credentials: "same-origin",
				signal: controller.signal,
			});
			if (response.status === 401) {
				onUnauthorized?.();
				return;
			}
			if (!response.ok || !response.body) {
				const text = await response.text();
				throw new Error(text || `Failed to open log stream (${response.status})`);
			}

			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			for (;;) {
				const { value, done } = await reader.read();
				if (done) {
					break;
				}
				setConsoleState((current) => ({
					...current,
					content: current.content + decoder.decode(value, { stream: true }),
				}));
			}
			setConsoleState((current) => ({ ...current, status: "done" }));
		} catch (error) {
			if (error.name === "AbortError") {
				setConsoleState((current) => ({
					...current,
					status: current.status === "done" ? "done" : "idle",
				}));
				return;
			}
			setConsoleState((current) => ({
				...current,
				status: "error",
				content: current.content + `\n[error] ${decodeError(error)}\n`,
			}));
		}
	};

	return {
		consoleState,
		showSnapshot,
		startJobStream,
		startTextStream,
		stopActiveStream,
	};
}
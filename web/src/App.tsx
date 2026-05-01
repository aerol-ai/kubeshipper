import React, { useEffect, useRef, useState } from "react";
import { BrowserRouter } from "react-router-dom";

import { Banner } from "./components/Banner";
import { LoadingScreen } from "./components/LoadingScreen";
import { useConsoleState } from "./hooks/useConsoleState";
import { requestJson, requestText, isAuthError } from "./lib/api";
import { decodeError } from "./lib/format";
import { AppRouter } from "./router";
import type { AuthSessionResponse, BannerState, SessionState } from "./types";

export function App() {
	const [session, setSession] = useState<SessionState>({
		loading: true,
		authenticated: false,
		mode: "jwt",
		version: "",
	});
	const [banner, setBanner] = useState<BannerState | null>(null);
	const bannerTimerRef = useRef<number | null>(null);
	const stopActiveStreamRef = useRef(() => {});

	const notify = (message: string, tone: BannerState["tone"] = "info") => {
		if (bannerTimerRef.current) {
			window.clearTimeout(bannerTimerRef.current);
		}
		setBanner({ message, tone });
		bannerTimerRef.current = window.setTimeout(() => setBanner(null), 4200);
	};

	const handleUnauthorized = () => {
		stopActiveStreamRef.current();
		setSession((current) => ({
			...current,
			loading: false,
			authenticated: false,
			mode: "jwt",
		}));
		notify("Session expired. Sign in again.", "error");
	};

	const consoleApi = useConsoleState({ onUnauthorized: handleUnauthorized });
	stopActiveStreamRef.current = consoleApi.stopActiveStream;

	const loadSession = async () => {
		try {
			const next = await requestJson<AuthSessionResponse>("/auth/session", { method: "GET" });
			setSession({
				loading: false,
				authenticated: !!next.authenticated,
				mode: next.mode || "jwt",
				version: next.version || "",
			});
		} catch (error) {
			if (isAuthError(error)) {
				setSession({ loading: false, authenticated: false, mode: "jwt", version: "" });
				return;
			}
			setSession({ loading: false, authenticated: false, mode: "jwt", version: "" });
			notify(`Session check failed: ${decodeError(error)}`, "error");
		}
	};

	useEffect(() => {
		loadSession();
		return () => {
			consoleApi.stopActiveStream();
			if (bannerTimerRef.current) {
				window.clearTimeout(bannerTimerRef.current);
			}
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const login = async (token: string) => {
		const result = await requestJson<AuthSessionResponse>("/auth/login", {
			method: "POST",
			body: JSON.stringify({ token }),
		});
		setSession({
			loading: false,
			authenticated: !!result.authenticated,
			mode: result.mode || "jwt",
			version: result.version || session.version || "",
		});
		notify("Dashboard session established.", "success");
	};

	const logout = async () => {
		try {
			await requestJson("/auth/logout", { method: "POST" });
		} catch {
			// Best-effort logout: local state is still cleared.
		}
		consoleApi.stopActiveStream();
		setSession((current) => ({
			...current,
			authenticated: false,
			mode: "jwt",
		}));
		notify("Session closed.", "info");
	};

	if (session.loading) {
		return <LoadingScreen />;
	}

	const pageProps = {
		session,
		logout,
		refreshSession: loadSession,
		requestJson,
		requestText,
		notify,
		onUnauthorized: handleUnauthorized,
		consoleState: consoleApi.consoleState,
		showSnapshot: consoleApi.showSnapshot,
		startJobStream: consoleApi.startJobStream,
		startTextStream: consoleApi.startTextStream,
		stopActiveStream: consoleApi.stopActiveStream,
	};

	return (
		<BrowserRouter>
			{banner ? <Banner tone={banner.tone} message={banner.message} /> : null}
			<AppRouter session={session} login={login} pageProps={pageProps} />
		</BrowserRouter>
	);
}
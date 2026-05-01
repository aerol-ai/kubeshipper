export type BannerTone = "info" | "success" | "error";

export interface SessionState {
	loading: boolean;
	authenticated: boolean;
	mode: string;
	version: string;
}

export interface BannerState {
	message: string;
	tone: BannerTone;
}

export interface AuthSessionResponse {
	authenticated: boolean;
	mode?: string;
	version?: string;
	expires_at?: string;
}

export interface ConsoleState {
	title: string;
	subtitle: string;
	kind: "events" | "text";
	status: string;
	entries: string[];
	content: string;
}

export interface ChartFormState {
	mode: string;
	release: string;
	namespace: string;
	sourceType: string;
	url: string;
	repoUrl: string;
	chart: string;
	version: string;
	ref: string;
	path: string;
	username: string;
	password: string;
	token: string;
	sshKeyPem: string;
	valuesText: string;
	timeoutSeconds: string;
	atomic: boolean;
	wait: boolean;
	reuseValues: boolean;
	resetValues: boolean;
	rollbackRevision: string;
	rolloutDeployment: string;
	rolloutService: string;
	rolloutContainer: string;
}

export interface ChartSourcePayload {
	type: string;
	url?: string;
	repoUrl?: string;
	chart?: string;
	version?: string;
	ref?: string;
	path?: string;
	tgzBase64?: string;
	auth?: {
		username?: string;
		password?: string;
		token?: string;
		sshKeyPem?: string;
	};
}

export interface RolloutWatchPayload {
	deployment?: string;
	service?: string;
	container?: string;
}

export type JsonRequest = <T = unknown>(path: string, options?: RequestInit) => Promise<T>;
export type TextRequest = (path: string, options?: RequestInit) => Promise<string>;
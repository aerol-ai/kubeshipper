import React from "react";
import { Navigate, Route, Routes } from "react-router-dom";

import { DashboardShell } from "./layout/DashboardShell";
import { AutomationsPage } from "./pages/AutomationsPage";
import { HelmPage } from "./pages/HelmPage";
import { LoginPage } from "./pages/LoginPage";
import { OverviewPage } from "./pages/OverviewPage";
import { ReleasesPage } from "./pages/ReleasesPage";
import { ServicesPage } from "./pages/ServicesPage";

export function AppRouter({ session, login, pageProps }) {
	return (
		<Routes>
			<Route
				path="/login"
				element={
					session.authenticated || session.mode === "open" ? (
						<Navigate replace to="/" />
					) : (
						<LoginPage login={login} version={session.version} />
					)
				}
			/>
			<Route
				path="/"
				element={
					session.authenticated || session.mode === "open" ? (
						<DashboardShell {...pageProps} />
					) : (
						<Navigate replace to="/login" />
					)
				}
			>
				<Route index element={<OverviewPage {...pageProps} />} />
				<Route path="helm" element={<HelmPage {...pageProps} />} />
				<Route path="releases" element={<ReleasesPage {...pageProps} />} />
				<Route path="services" element={<ServicesPage {...pageProps} />} />
				<Route path="automations" element={<AutomationsPage {...pageProps} />} />
			</Route>
			<Route path="*" element={<Navigate replace to="/" />} />
		</Routes>
	)
}
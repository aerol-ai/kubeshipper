import React from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";

import { classNames } from "../lib/format";
import { NAV_ITEMS, PAGE_META } from "../constants";

export function DashboardShell(props) {
	const location = useLocation();
	const meta = PAGE_META[location.pathname] || PAGE_META["/"];

	return (
		<div className="shell">
			<aside className="shell-sidebar">
				<div className="brand-mark">
					<h1 className="brand-title">KubeShipper</h1>
				</div>
				<nav className="nav-list">
					{NAV_ITEMS.map((item) => (
						<NavLink
							key={item.to}
							to={item.to}
							end={item.to === "/"}
							className={({ isActive }) => classNames("nav-link", isActive && "active")}
						>
							<span className="nav-label">{item.label}</span>
							<span className="nav-caption">{item.caption}</span>
						</NavLink>
					))}
				</nav>
				<div className="sidebar-footer">
					<div className="button-row" style={{ justifyContent: "space-between", alignItems: "center" }}>
						<span className="session-pill">Auth mode: {props.session.mode}</span>
						<button className="ghost-button" type="button" onClick={props.logout}>
							Sign out
						</button>
					</div>
					<p className="meta-copy" style={{ marginTop: 14 }}>
						Version <span className="mono">{props.session.version || "dev"}</span>
					</p>
				</div>
			</aside>
			<main className="shell-main">
				<header className="topbar">
					<div>
						<h2 className="page-title">{meta.title}</h2>
						<p className="page-subtitle">{meta.subtitle}</p>
					</div>
					<div className="topbar-meta">
						<button className="subtle-button" type="button" onClick={props.refreshSession}>
							Refresh session
						</button>
					</div>
				</header>
				<Outlet />
			</main>
			<aside className="shell-console">
				<div className="console-header">
					<div>
						<h3 className="console-title">{props.consoleState.title}</h3>
						<p className="console-subtitle">{props.consoleState.subtitle}</p>
					</div>
					<div className="button-row">
						<span className={classNames(
							"state-pill",
							props.consoleState.status === "error"
								? "danger"
								: props.consoleState.status === "streaming"
									? "warning"
									: "success",
						)}>
							{props.consoleState.status}
						</span>
						<button className="ghost-button" type="button" onClick={props.stopActiveStream}>
							Stop
						</button>
					</div>
				</div>
				{props.consoleState.kind === "text" ? (
					<pre className="console-content">{props.consoleState.content}</pre>
				) : (
					<div className="console-content console-list">
						{props.consoleState.entries.map((entry, index) => (
							<div
								key={`${entry}-${index}`}
								className={classNames("console-line", entry.toLowerCase().includes("error") && "error")}
							>
								{entry}
							</div>
						))}
					</div>
				)}
			</aside>
		</div>
	);
}
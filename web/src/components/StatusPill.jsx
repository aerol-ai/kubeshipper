import React from "react";

import { classNames } from "../lib/format";

export function StatusPill({ status }) {
	const normalized = String(status || "unknown").toLowerCase();
	const tone = normalized.includes("fail") || normalized.includes("error")
		? "danger"
		: normalized.includes("pending") || normalized.includes("deploy") || normalized.includes("drift")
			? "warning"
			: "success";

	return <span className={classNames("state-pill", tone)}>{status || "unknown"}</span>;
}
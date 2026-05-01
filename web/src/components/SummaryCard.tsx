import React from "react";

export function SummaryCard({ label, value, note }) {
	return (
		<div className="summary-card">
			<div className="summary-label">{label}</div>
			<div className="summary-value">{value}</div>
			<div className="summary-footnote">{note}</div>
		</div>
	);
}
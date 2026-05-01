import React from "react";

export function EmptyState({ title, copy }) {
	return (
		<div className="empty-state">
			<h4 className="empty-title">{title}</h4>
			<p className="empty-copy">{copy}</p>
		</div>
	);
}
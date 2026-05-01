import React from "react";

export function Banner({ tone, message }) {
	return <div className={`banner ${tone}`}>{message}</div>;
}
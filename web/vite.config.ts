import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
	plugins: [react()],
	base: "/",
	server: {
		host: "0.0.0.0",
		port: 5173,
		proxy: {
			"/api": {
				target: "http://localhost:3000",
				changeOrigin: true,
			},
		},
	},
	preview: {
		host: "0.0.0.0",
		port: 4173,
	},
	build: {
		outDir: "../internal/ui/static",
		emptyOutDir: true,
		assetsDir: "assets",
	},
});
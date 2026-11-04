import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The build lands in ../src/web, which src/server.go embeds with //go:embed web.
// It is committed so `go install` and `go build` work without Node.
export default defineConfig({
  plugins: [react()],
  build: { outDir: '../src/web', emptyOutDir: true },
  // `npm run dev` talks to a dashboard running on the default address.
  server: { proxy: { '/api': 'http://127.0.0.1:7878' } },
});

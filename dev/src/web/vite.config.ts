import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';
const projectRoot = process.env.P11_PROJECT_ROOT;
if (!projectRoot) throw new Error('Use dev/tools/前端开发.py');
export default defineConfig({ plugins: [react()], base: '/web/',
  define: { P11_API_BASE: JSON.stringify(process.env.P11_FRONTEND_API_BASE || '') },
  build: { outDir: path.join(projectRoot, 'runtime/frontend/builds/web'), emptyOutDir: true, sourcemap: false },
  server: { host: '127.0.0.1', fs: { strict: true } }
});

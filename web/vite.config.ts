import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@ui': fileURLToPath(new URL('../ui/react', import.meta.url)) },
    dedupe: ['react', 'react-dom'],
  },
  build: { sourcemap: false, target: 'es2022' },
});

import { sveltekit } from '@sveltejs/kit/vite';
import { execSync } from 'node:child_process';
import { defineConfig } from 'vite';

function uiBuildHash() {
  if (process.env.PHOTOSTORE_UI_BUILD_HASH) {
    return process.env.PHOTOSTORE_UI_BUILD_HASH;
  }
  try {
    const hash = execSync('git rev-parse --short=12 HEAD', { encoding: 'utf8' }).trim();
    const dirty = execSync('git status --porcelain', { encoding: 'utf8' }).trim() ? '-dirty' : '';
    return `${hash}${dirty}`;
  } catch {
    return 'unknown';
  }
}

export default defineConfig({
  define: {
    __PHOTOSTORE_UI_BUILD_HASH__: JSON.stringify(uiBuildHash())
  },
  plugins: [sveltekit()],
  server: {
    proxy: {
      '/api': {
        target: process.env.PHOTOSTORE_API_BASE ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
        ws: true
      }
    }
  }
});

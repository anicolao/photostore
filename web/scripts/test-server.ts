import { mkdirSync, rmSync } from 'node:fs';
import { spawn } from 'node:child_process';

const store = '/tmp/photostore-e2e-store';
rmSync(store, { recursive: true, force: true });
mkdirSync(store, { recursive: true });
const apiAddr = '127.0.0.1:18080';
const env = {
  ...process.env,
  PHOTOSTORE_API_BASE: `http://${apiAddr}`,
  PHOTOSTORE_E2E_STORE: store,
  PHOTOSTORE_DETERMINISTIC_IDS: '1',
  PHOTOSTORE_FIXED_NOW_MS: '1710504000000',
  PHOTOSTORE_SCAN_WORKERS: '1',
  PHOTOSTORE_THUMBNAIL_WORKERS: '1',
  PHOTOSTORE_DEDUP_WORKERS: '1',
  PHOTOSTORE_METADATA_WORKERS: '1'
};

const children: ReturnType<typeof spawn>[] = [];

function run(name: string, cmd: string, args: string[], options = {}) {
  const child = spawn(cmd, args, { stdio: 'inherit', env, ...options });
  children.push(child);
  child.on('exit', (code) => {
    if (code && !shuttingDown) {
      console.error(`${name} exited with ${code}`);
      shutdown(code);
    }
  });
  return child;
}

let shuttingDown = false;
function shutdown(code = 0) {
  shuttingDown = true;
  for (const child of children) {
    if (!child.killed) child.kill('SIGTERM');
  }
  rmSync(store, { recursive: true, force: true });
  process.exit(code);
}

process.on('SIGINT', () => shutdown(130));
process.on('SIGTERM', () => shutdown(143));

run('init', 'go', ['run', '../cmd/photostore', 'init', '--store', store]).on('exit', (code) => {
  if (code !== 0) shutdown(code ?? 1);
  run('api', 'go', ['run', '../cmd/photostore', 'serve', '--store', store, '--addr', apiAddr, '--api-only']);
  run('vite', 'bun', ['run', 'dev']);
});

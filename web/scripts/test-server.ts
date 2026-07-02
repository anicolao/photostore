import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawn } from 'node:child_process';

const store = mkdtempSync(join(tmpdir(), 'photostore-e2e-store-'));
const apiAddr = '127.0.0.1:18080';
const env = { ...process.env, PHOTOSTORE_API_BASE: `http://${apiAddr}`, PHOTOSTORE_E2E_STORE: store };

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

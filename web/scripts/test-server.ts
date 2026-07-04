import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { spawn, spawnSync } from 'node:child_process';
import { createServer } from 'node:http';

const store = '/tmp/photostore-e2e-store';
rmSync(store, { recursive: true, force: true });
mkdirSync(store, { recursive: true });
const apiAddr = '127.0.0.1:18080';
const tileAddr = '127.0.0.1:18081';
const tilePNG = generateTilePNG();
const env = {
  ...process.env,
  PHOTOSTORE_API_BASE: `http://${apiAddr}`,
  PHOTOSTORE_MAP_TILE_URL_TEMPLATE: `http://${tileAddr}/{z}/{x}/{y}.png`,
  PHOTOSTORE_E2E_STORE: store,
  PHOTOSTORE_DETERMINISTIC_IDS: '1',
  PHOTOSTORE_ALLOW_DETERMINISTIC_IDS: '1',
  PHOTOSTORE_FIXED_NOW_MS: '1710504000000',
  PHOTOSTORE_UI_BUILD_HASH: 'e2e-build',
  PHOTOSTORE_SCAN_WORKERS: '1',
  PHOTOSTORE_THUMBNAIL_WORKERS: '1',
  PHOTOSTORE_DEDUP_WORKERS: '1',
  PHOTOSTORE_METADATA_WORKERS: '1'
};

const children: ReturnType<typeof spawn>[] = [];
const tileServer = createServer((_req, res) => {
  res.writeHead(200, {
    'content-type': 'image/png',
    'cache-control': 'public, max-age=604800'
  });
  res.end(tilePNG);
});

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
  tileServer.close();
  for (const child of children) {
    if (!child.killed) child.kill('SIGTERM');
  }
  rmSync(store, { recursive: true, force: true });
  process.exit(code);
}

process.on('SIGINT', () => shutdown(130));
process.on('SIGTERM', () => shutdown(143));

tileServer.listen(18081, '127.0.0.1');

run('init', 'go', ['run', '../cmd/photostore', 'init', '--store', store]).on('exit', (code) => {
  if (code !== 0) shutdown(code ?? 1);
  run('api', 'go', ['run', '../cmd/photostore', 'serve', '--store', store, '--addr', apiAddr, '--api-only']);
  run('vite', 'bun', ['run', 'dev']);
});

function generateTilePNG() {
  const source = `
package main
import (
  "image"
  "image/color"
  "image/draw"
  "image/png"
  "os"
)
func main() {
  img := image.NewRGBA(image.Rect(0,0,256,256))
  draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{0xd8,0xee,0xf7,0xff}), image.Point{}, draw.Src)
  draw.Draw(img, image.Rect(0,150,256,256), image.NewUniform(color.RGBA{0xd7,0xea,0xd1,0xff}), image.Point{}, draw.Src)
  green := color.RGBA{0x9e,0xc0,0x95,0xff}
  white := color.RGBA{0xff,0xff,0xff,0xff}
  for x:=0; x<256; x++ {
    y := 170 - x*80/256
    for dy:=-6; dy<=6; dy++ {
      if y+dy >=0 && y+dy < 256 { img.Set(x,y+dy,green) }
    }
  }
  for _, v := range []int{64,128,192} {
    for i:=0; i<256; i++ {
      img.Set(i,v,white)
      img.Set(v,i,white)
      img.Set(i,v+1,white)
      img.Set(v+1,i,white)
    }
  }
  if err := png.Encode(os.Stdout,img); err != nil { panic(err) }
}
`;
  const sourcePath = '/tmp/photostore-e2e-tile.go';
  writeFileSync(sourcePath, source);
  const result = spawnSync('go', ['run', sourcePath], {
    env: process.env,
    maxBuffer: 1024 * 1024
  });
  rmSync(sourcePath, { force: true });
  if (result.status !== 0) {
    process.stderr.write(result.stderr);
    throw new Error(`tile fixture generation failed with ${result.status}`);
  }
  return result.stdout;
}

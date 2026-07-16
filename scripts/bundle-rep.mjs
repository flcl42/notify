import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

const packageJson = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));

await build({
  entryPoints: [fileURLToPath(new URL('../cli/rep.js', import.meta.url))],
  bundle: true,
  platform: 'node',
  format: 'cjs',
  outfile: fileURLToPath(new URL('../dist/rep.cjs', import.meta.url)),
  define: {
    __REP_VERSION__: JSON.stringify(packageJson.version)
  }
});

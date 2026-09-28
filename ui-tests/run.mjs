// Runs the @ui scenarios once per viewport width; fails if any width fails.
import { spawnSync } from 'node:child_process';

const widths = (process.env.WIDTHS || '375,768,1280').split(',');
let failed = [];
for (const w of widths) {
  console.log(`\n=== ${w}px ===`);
  const r = spawnSync('npx', ['cucumber-js'], { stdio: 'inherit', env: { ...process.env, VIEWPORT: w } });
  if (r.status !== 0) failed.push(w);
}
if (failed.length) { console.error(`\nUI scenarios failed at: ${failed.join(', ')}px`); process.exit(1); }
console.log(`\nAll UI scenarios passed at ${widths.join(', ')}px`);

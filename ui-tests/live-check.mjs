// Read-only smoke test of the LIVE site (never writes: no admin sign-in, no
// picks). Run after each deploy:
//   SITE=https://hockeypool.opeongo.net GUEST_PHRASE=... node live-check.mjs
// Without GUEST_PHRASE the guest board checks are skipped.
import { chromium } from 'playwright';

const site = process.env.SITE || 'https://hockeypool.opeongo.net';
const phrase = process.env.GUEST_PHRASE || '';
let fails = 0;
const check = (name, ok, extra = '') => { console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? '  ' + extra : ''}`); if (!ok) fails++; };

// Plain HTTP checks (also proves the TLS certificate is trusted).
const health = await fetch(site + '/healthz');
check('health check', health.ok && (await health.text()) === 'ok');
const root = await fetch(site + '/', { redirect: 'manual' });
check('signed-out visitors are sent to /login', root.status === 303 && root.headers.get('location') === '/login');
check('no browser sign-in pop-up requested', !root.headers.get('www-authenticate'));
const apiState = await fetch(site + '/api/state');
check('the API is locked when signed out', apiState.status === 401);

const browser = await chromium.launch();
for (const width of [375, 1280]) {
  const ctx = await browser.newContext({ viewport: { width, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  page.on('pageerror', e => errors.push(e.message));
  page.on('dialog', d => { errors.push('pop-up: ' + d.message()); d.dismiss(); });
  await page.goto(site + '/');
  check(`${width}px sign-in page loads`, page.url().endsWith('/login') && await page.isVisible('#guest-btn'));
  await page.click('#guest-btn');
  check(`${width}px guest form opens in place`, await page.isVisible('input[name=phrase]'));
  if (phrase) {
    await page.fill('input[name=phrase]', phrase);
    await page.locator('input[name=name]').first().fill('live-check');
    await page.click('button.primary:text-is("Enter as guest")');
    await page.waitForSelector('#players tbody tr[data-rid]', { timeout: 20000 });
    const s = await (await page.request.get(site + '/api/state')).json();
    check(`${width}px guest board loads`, s.players.length > 100, `${s.players.length} players, ${s.picks.length} picks, ${s.managers.length} teams`);
    check(`${width}px guest gets no private flags`, !s.players.some(p => p.tag || p.why));
    await page.fill('#q', 'sto'); await page.waitForTimeout(200);
    check(`${width}px search works`, (await page.locator('#players td.name').allTextContents()).some(t => t.includes('Stone')));
    await page.fill('#q', '');
    await page.waitForTimeout(2500); // one live-update cycle
    check(`${width}px no sideways scroll`, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
  }
  check(`${width}px no console errors, exceptions or pop-ups`, errors.length === 0, errors.join(' | '));
  await ctx.close();
}
await browser.close();
console.log(fails ? `\n${fails} live check(s) FAILED` : '\nLive site OK');
process.exit(fails ? 1 : 0);

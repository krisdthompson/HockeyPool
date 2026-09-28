// Starts a real build of the server, and gives every scenario a fresh
// browser page and a reset draft.
import { BeforeAll, AfterAll, Before, After, setWorldConstructor, setDefaultTimeout } from '@cucumber/cucumber';
import { chromium } from 'playwright';
import { execFileSync, spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

setDefaultTimeout(30_000);

export const ADMIN_PW = 'admin-pw';
export const GUEST_PHRASE = 'guest-phrase';
const repo = path.resolve(import.meta.dirname, '../..');
const port = 18000 + Math.floor(Math.random() * 1000);
export const base = `http://127.0.0.1:${port}`;
const basic = 'Basic ' + Buffer.from(`kris:${ADMIN_PW}`).toString('base64');

let server, browser, defaults;

// The admin API, used to set up and inspect state (basic auth; browsers never see it).
export async function api(p, body) {
  const r = await fetch(base + '/api/' + p, body === undefined ? { headers: { Authorization: basic } }
    : { method: 'POST', headers: { Authorization: basic, 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const j = await r.json();
  if (!r.ok) throw new Error(`${p}: ${j.error}`);
  return j;
}
export const state = () => api('state');

BeforeAll(async function () {
  const tmp = path.join(repo, 'ui-tests', '.tmp');
  mkdirSync(tmp, { recursive: true });
  const bin = path.join(tmp, 'hockeypool');
  execFileSync('go', ['build', '-o', bin, '.'], { cwd: repo, stdio: 'inherit' });
  const hash = pw => execFileSync(bin, ['hash', pw]).toString().trim();
  server = spawn(bin, [], {
    env: { ...process.env, PORT: String(port), DATA_DIR: mkdtempSync(path.join(tmpdir(), 'hp-ui-')),
      DRAFT_ADMIN_USER: 'kris', DRAFT_ADMIN_HASH: hash(ADMIN_PW), DRAFT_GUEST_HASH: hash(GUEST_PHRASE), DRAFT_PASSWORD: '' },
    stdio: 'ignore',
  });
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch(base + '/healthz')).ok) break; } catch {}
    await new Promise(r => setTimeout(r, 100));
  }
  const s = await state();
  defaults = { managers: s.managers, me: 0, rosterSize: 7, snake: true, maxTeams: 0, budget: 100, minBid: 4 };
  browser = await chromium.launch();
});

AfterAll(async function () {
  await browser?.close();
  server?.kill();
});

class World {
  constructor() { this.popups = 0; this.bag = {}; }
  get width() { return +(process.env.VIEWPORT || 1280); }
  async open(url = '/') {
    this.context = await browser.newContext({ viewport: { width: this.width, height: 900 } });
    // Count any modal the page tries to open (there should never be one).
    await this.context.addInitScript(() => {
      window.__modals = 0;
      for (const f of ['alert', 'confirm', 'prompt']) window[f] = () => { window.__modals++; return false; };
    });
    this.page = await this.context.newPage();
    this.page.on('dialog', d => { this.popups++; d.dismiss(); });
    await this.page.goto(base + url);
  }
  async signIn() {
    await this.open('/login');
    await this.page.locator('input[name=name]').last().fill('kris');
    await this.page.fill('input[name=password]', ADMIN_PW);
    await this.page.click('button.primary:text-is("Sign in")');
    await this.page.waitForURL(base + '/');
    await this.page.waitForSelector('#players tbody tr[data-rid]');
  }
  async idOf(name) {
    const p = (await state()).players.find(p => p.name === name);
    if (!p) throw new Error(`no player ${name}`);
    return p.id;
  }
  async reloadBoard() {
    await this.page.reload();
    await this.page.waitForSelector('#players tbody tr[data-rid]');
  }
}
setWorldConstructor(World);

Before(async function () {
  // Every scenario starts from nobody drafted, default settings, the official list.
  await api('reset', {});
  await api('settings', defaults);
  await api('reseed', {});
});

After(async function () {
  await this.context?.close();
});

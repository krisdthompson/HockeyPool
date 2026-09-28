import { Given, When, Then } from '@cucumber/cucumber';
import assert from 'node:assert/strict';
import { api, state, base } from './world.mjs';

const wait = ms => new Promise(r => setTimeout(r, ms));
// Poll until check() passes (live saves are asynchronous).
async function eventually(check, what, ms = 3000) {
  let last;
  for (const end = Date.now() + ms; Date.now() < end; await wait(100)) {
    try { if (await check()) return; } catch (e) { last = e; }
  }
  throw new Error(`timed out waiting for ${what}${last ? ': ' + last.message : ''}`);
}
const pickOf = async id => (await state()).picks.find(k => k.playerId === id);
const teamIndex = async name => (await state()).managers.indexOf(name);
const listRow = (w, id) => w.page.locator(`#players tr[data-rid="players-${id}"]`);
const panel = (w, id) => w.page.locator(`#${w.bag.where || 'players'} [data-pd="${id}"]`);
const recsIds = w => w.page.evaluate(() => [...document.querySelectorAll('#recs tr[data-rid]')].map(t => t.dataset.rid).join(','));

// Make sure a player's row is in the list (search for him if drafted players are hidden).
async function showRow(w, id, name) {
  if (await listRow(w, id).count()) return;
  await w.page.fill('#q', name.split(' ').slice(1).join(' '));
  await listRow(w, id).waitFor();
}

Given('Kris is signed in on the board', async function () { await this.signIn(); });

// Layout.
Then('Best buys, the player list, my team, recent picks and the draft board are stacked full width', async function () {
  const boxes = await this.page.evaluate(() => ['#recs', '#mytitle', '#recent', '#boardsec']
    .map(s => document.querySelector(s).closest('section').getBoundingClientRect())
    .map(r => ({ x: Math.round(r.x), w: Math.round(r.width), top: r.top })));
  assert.ok(boxes.every(b => b.x === boxes[0].x && Math.abs(b.w - boxes[0].w) <= 1), JSON.stringify(boxes));
  assert.ok(boxes.every((b, i) => i === 0 || b.top > boxes[i - 1].top), 'sections are stacked in order');
});
Then('the draft board spans the full page width', async function () {
  const [board, main] = await this.page.evaluate(() => [document.querySelector('#boardsec').getBoundingClientRect().width,
    document.querySelector('main').clientWidth - 32]);
  assert.ok(Math.abs(board - main) <= 1, `board ${board}px, page ${main}px`);
  assert.ok(await this.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'no sideways page scroll');
});
Then('"Ranking weights" is collapsed', async function () { assert.equal(await this.page.getAttribute('#weights', 'open'), null); });
When(/^Kris (expands|collapses) "Ranking weights"$/, async function (_how) { await this.page.click('#weights > summary'); });
Then(/^the weight sliders are (shown|hidden)$/, async function (how) {
  assert.equal(await this.page.isVisible('#w-espn'), how === 'shown');
});
Then('the first column of the player list and of Best buys is headed "Drafted"', async function () {
  assert.equal((await this.page.textContent('#players thead th')).trim(), 'Drafted');
  assert.equal((await this.page.textContent('#recs thead th')).trim(), 'Drafted');
});

// Ticking.
async function tick(w, name) {
  w.bag.id = await w.idOf(name); w.bag.name = name; w.bag.where = 'players';
  await showRow(w, w.bag.id, name);
  await listRow(w, w.bag.id).locator('input.gone').click();
}
When(/^Kris ticks Drafted on "([^"]*)"(?: in the player list)?$/, async function (name) {
  await tick(this, name);
  await panel(this, this.bag.id).waitFor();
});
Then('a details row opens directly below his row', async function () {
  const ok = await this.page.evaluate(id => !!document.querySelector(`#players tr[data-rid="players-${id}"]`).nextElementSibling?.querySelector(`[data-pd="${id}"]`), this.bag.id);
  assert.ok(ok);
});
Then('it has an Owner choice of pool teams, an Amount and a Dismiss button', async function () {
  const p = panel(this, this.bag.id);
  const opts = await p.locator('[data-pf=mgr] option').allTextContents();
  for (const m of (await state()).managers) assert.ok(opts.includes(m), `owner choice has ${m}`);
  assert.ok(await p.locator('[data-pf=price]').isVisible());
  assert.ok(await p.locator('[data-act=pskip]').isVisible());
  assert.equal(await p.locator('button', { hasText: /^Save$/ }).count(), 0, 'no save button');
});
When(/^Kris (?:chooses owner|changes the owner to) "([^"]*)"$/, async function (team) {
  this.bag.expect = { manager: await teamIndex(team) };
  await panel(this, this.bag.id).locator('[data-pf=mgr]').selectOption({ label: team });
});
When(/^Kris (?:enters an amount of|changes the amount to) \$(\d+)$/, async function (amt) {
  this.bag.expect = { price: +amt };
  const f = panel(this, this.bag.id).locator('[data-pf=price]');
  await f.fill(String(amt)); await f.press('Enter');
});
Then(/^the (?:owner|amount|change) is saved (?:without pressing a save button|automatically)$/, async function () {
  await eventually(async () => {
    const k = await pickOf(this.bag.id);
    return Object.entries(this.bag.expect).every(([f, v]) => k?.[f] === v);
  }, `the pick to be saved as ${JSON.stringify(this.bag.expect)}`);
});
When(/^Kris (?:presses Dismiss|dismisses the details)$/, async function () {
  await panel(this, this.bag.id).locator('[data-act=pskip]').click();
});
When('Kris saves an owner and an amount', async function () {
  const p = panel(this, this.bag.id);
  await p.locator('[data-pf=mgr]').selectOption({ index: 2 });
  await wait(300);
  await p.locator('[data-pf=price]').fill('20'); await p.locator('[data-pf=price]').press('Enter');
});
Then(/^the details (?:row )?close(?:s)?(?: without asking)?$/, async function () {
  await eventually(async () => !(await this.page.locator(`[data-pd="${this.bag.id}"]`).count()), 'the details to close');
  assert.equal(await this.page.locator(`[data-ud="${this.bag.id}"]`).count(), 0, 'no undraft question');
});
Then('the player leaves the list because drafted players are hidden', async function () {
  assert.equal(await listRow(this, this.bag.id).count(), 0);
});

// No layout shift.
When('Kris ticks Drafted on a player partway down the list', async function () {
  const rid = await this.page.evaluate(() => document.querySelectorAll('#players tr[data-rid]')[12].dataset.rid);
  this.bag.id = +rid.split('-')[1]; this.bag.where = 'players';
  const row = listRow(this, this.bag.id);
  await row.evaluate(el => el.scrollIntoView({ block: 'center' }));
  await wait(100);
  this.bag.top = (await row.boundingBox()).y;
  await row.locator('input.gone').click();
  await panel(this, this.bag.id).waitFor();
  await wait(300);
});
Then("that player's row stays exactly where it was on screen", async function () {
  const y = (await listRow(this, this.bag.id).boundingBox()).y;
  assert.ok(Math.abs(y - this.bag.top) < 2, `moved from ${this.bag.top} to ${y}`);
});
Then('the details fold open below it', async function () {
  assert.ok(await this.page.evaluate(id => !!document.querySelector(`#players tr[data-rid="players-${id}"]`).nextElementSibling?.querySelector('.editor.pend'), this.bag.id));
});

// Best buys waits.
Given(/^"([^"]*)" is in Best buys$/, async function (name) {
  const id = await this.idOf(name);
  this.bag.recs0 = await recsIds(this);
  assert.ok(this.bag.recs0.split(',').includes(`recs-${id}`), `${name} not in Best buys: ${this.bag.recs0}`);
});
Then('Best buys still shows the same players in the same order', async function () {
  await wait(2300); // across a live update
  assert.equal(await recsIds(this), this.bag.recs0);
});
Then(/^Best buys updates and "([^"]*)" is no longer in it$/, async function (name) {
  const id = await this.idOf(name);
  await eventually(async () => !(await recsIds(this)).split(',').includes(`recs-${id}`), 'Best buys to update');
});

// Unticking.
When('Kris unticks him before dismissing the details', async function () {
  await listRow(this, this.bag.id).locator('input.gone').click();
});
Then(/^"([^"]*)" is not drafted and is still in the list$/, async function (name) {
  const id = await this.idOf(name);
  await eventually(async () => !(await pickOf(id)), `${name} to be undrafted`);
  assert.ok(await listRow(this, id).isVisible());
  assert.equal(await listRow(this, id).locator('input.gone').isChecked(), false);
});
Then(/^"([^"]*)" is drafted with no owner recorded$/, async function (name) {
  assert.equal((await pickOf(await this.idOf(name)))?.manager, -1);
});
Given(/^"([^"]*)" was drafted by "([^"]*)" for \$(\d+)$/, async function (name, team, amt) {
  this.bag.id = await this.idOf(name); this.bag.name = name;
  await api('pick', { playerId: this.bag.id, manager: await teamIndex(team), price: +amt });
  await this.reloadBoard();
});
Given(/^"([^"]*)" is drafted$/, async function (name) {
  this.bag.id = await this.idOf(name); this.bag.name = name;
  await api('pick', { playerId: this.bag.id, manager: -1 });
  await this.reloadBoard();
});
When('Kris presses the pencil next to him', async function () {
  this.bag.where = 'recent';
  await this.page.click(`#recent [data-pencil="${this.bag.id}"]`);
});
When('Kris clicks him on the draft board', async function () {
  this.bag.where = 'boardsec';
  await this.page.click(`#boardgrid a[data-pencil="${this.bag.id}"]`);
});
Then(/^the details (?:row opens below him|open under the draft board) showing "([^"]*)" and \$(\d+)$/, async function (team, amt) {
  const p = panel(this, this.bag.id);
  await p.waitFor();
  assert.equal(await p.locator('[data-pf=mgr]').inputValue(), String(await teamIndex(team)));
  assert.equal(await p.locator('[data-pf=price]').inputValue(), String(amt));
  if (this.bag.where === 'boardsec') assert.equal(await this.page.locator('#boardedit [data-pd]').count(), 1);
});
When(/^Kris unticks Drafted on "([^"]*)"$/, async function (name) {
  await showRow(this, this.bag.id, name);
  await listRow(this, this.bag.id).locator('input.gone').click();
});
Then(/^he stays drafted and "([^"]*)" appears below his row$/, async function (text) {
  const q = this.page.locator(`[data-ud="${this.bag.id}"]`);
  await q.waitFor();
  assert.ok((await q.textContent()).replace(/\s+/g, ' ').includes(text));
  assert.ok(await listRow(this, this.bag.id).locator('input.gone').isChecked());
  assert.ok(await pickOf(this.bag.id));
});
Then('his row stays in the list while the question is showing', async function () {
  await wait(2300);
  assert.ok(await listRow(this, this.bag.id).isVisible());
  assert.ok(await this.page.locator(`[data-ud="${this.bag.id}"]`).isVisible());
});
When('Kris presses Cancel', async function () {
  await this.page.locator('button:visible', { hasText: /^Cancel$/ }).first().click();
});
Then('he is still drafted', async function () { assert.ok(await pickOf(this.bag.id)); });
When('Kris unticks him again and presses "Yes, undraft"', async function () {
  await listRow(this, this.bag.id).locator('input.gone').click();
  await this.page.click(`[data-ud="${this.bag.id}"] [data-act=udyes]`);
});
Then('he is back on the board', async function () {
  await eventually(async () => !(await pickOf(this.bag.id)), 'him to be undrafted');
});
When('Kris turns off "Hide drafted"', async function () { await this.page.uncheck('#fhidedrafted'); });
Then(/^"([^"]*)" is in the list with his Drafted box ticked$/, async function (name) {
  assert.ok(await listRow(this, await this.idOf(name)).locator('input.gone').isChecked());
});

// Mine and the editor.
When('Kris presses Mine on the top player in Best buys', async function () {
  const b = this.page.locator('#recs tr[data-rid] button[data-mine]').first();
  this.bag.id = +(await b.getAttribute('data-mine'));
  await b.click();
});
Then("an editor opens under that player with Kris's team as owner and the cursor in Amount", async function () {
  const ed = this.page.locator(`#recs [data-ed="${this.bag.id}"]`);
  await ed.waitFor();
  assert.equal(await ed.locator('[data-f=mgr]').inputValue(), String((await state()).me));
  assert.equal(await this.page.evaluate(() => document.activeElement?.dataset.f), 'price');
});
When(/^Kris types an amount of \$(\d+) and presses Enter$/, async function (amt) {
  await this.page.keyboard.type(String(amt)); await this.page.keyboard.press('Enter');
});
Then(/^the buy is recorded for Kris's team at \$(\d+)$/, async function (amt) {
  const me = (await state()).me;
  await eventually(async () => { const k = await pickOf(this.bag.id); return k?.manager === me && k.price === +amt; }, 'the buy');
});
Then(/^Kris's team shows (\d+) of (\d+) players and \$(\d+) left$/, async function (n, of, left) {
  await eventually(async () => { const t = await this.page.textContent('#mytitle'); return t.includes(`(${n}/${of})`) && t.includes(`$${left} left`); }, 'my team');
});
When(/^Kris clicks "([^"]*)"( again)?$/, async function (name, _again) {
  this.bag.id = await this.idOf(name);
  await showRow(this, this.bag.id, name);
  await listRow(this, this.bag.id).locator('a[data-edit]').click();
});
Then('an editor opens directly under his row', async function () {
  assert.ok(await this.page.evaluate(id => !!document.querySelector(`#players tr[data-rid="players-${id}"]`).nextElementSibling?.querySelector(`[data-ed="${id}"]`), this.bag.id));
  assert.equal(await listRow(this, this.bag.id).locator('a[data-edit]').getAttribute('aria-expanded'), 'true');
});
Then('the editor closes', async function () {
  assert.equal(await this.page.locator('[data-ed]').count(), 0);
  assert.equal(await listRow(this, this.bag.id).locator('a[data-edit]').getAttribute('aria-expanded'), 'false');
});
When(/^Kris sets his injury to "([^"]*)" in the editor$/, async function (text) {
  this.bag.injury = text;
  const f = this.page.locator(`#players [data-ed="${this.bag.id}"] [data-f=injury]`);
  await f.fill(text); await f.press('Enter');
});
Then('the injury is saved without pressing a save button', async function () {
  await eventually(async () => (await state()).players.find(p => p.id === this.bag.id).injury === this.bag.injury, 'the injury to save');
});
Given("Kris is typing in a player's editor", async function () {
  this.bag.id = await this.idOf('Mark Stone');
  await showRow(this, this.bag.id, 'Mark Stone');
  await listRow(this, this.bag.id).locator('a[data-edit]').click();
  await this.page.locator(`[data-ed="${this.bag.id}"] [data-f=note]`).fill('typed but not saved');
});
When('another pick arrives from the server', async function () {
  await api('pick', { playerId: await this.idOf('Nathan MacKinnon'), manager: -1 });
  await wait(2500); // longer than the live-update interval
});
Then('what Kris typed is still there', async function () {
  assert.equal(await this.page.inputValue(`[data-ed="${this.bag.id}"] [data-f=note]`), 'typed but not saved');
});

// Confirmations and modals.
When('Kris presses "Clear all picks"', async function () {
  await this.page.click('#tab-setup'); await this.page.click('#s-reset');
});
Then('an inline "Yes" and "Cancel" appear next to it', async function () {
  assert.ok(await this.page.isVisible('#c-reset [data-yes]'));
  assert.ok(await this.page.isVisible('#c-reset [data-no]'));
});
Then(/^no browser pop-up opens$/, async function () {
  assert.equal(this.popups, 0);
  assert.equal(await this.page.evaluate(() => window.__modals), 0);
});
Then('the page has no dialog elements', async function () {
  assert.equal(await this.page.locator('dialog').count(), 0);
});
Then('nothing calls confirm, prompt or alert', async function () {
  const html = await this.page.content();
  assert.equal(/(^|[^.\w])(confirm|prompt|alert)\s*\(/.test(html), false);
});

// Search.
When(/^(?:Kris|they) types? "([^"]*)" in the search box$/, async function (q) {
  await this.page.fill('#q', q); await wait(150);
});
Then('the list is not filtered and no suggestions are offered', async function () {
  const filtered = await this.page.textContent('#count');
  await this.page.fill('#q', ''); await wait(150);
  assert.equal(filtered, await this.page.textContent('#count'));
  await this.page.fill('#q', 's'); await wait(150);
  assert.equal(await this.page.locator('#qlist option').count(), 0);
});
Then(/^the list shows "([^"]*)"$/, async function (name) {
  assert.ok((await this.page.locator('#players tbody td.name').allTextContents()).some(t => t.includes(name)));
});
Then(/^the suggestions include "([^"]*)"$/, async function (v) {
  const vals = await this.page.locator('#qlist option').evaluateAll(os => os.map(o => o.value));
  assert.ok(vals.includes(v), vals.join(','));
});
Then(/^every player listed plays for "([^"]*)"$/, async function (team) {
  const teams = await this.page.locator('#players tbody tr[data-rid]').evaluateAll(rs => rs.map(r => r.querySelectorAll('td')[3].textContent.trim()));
  assert.ok(teams.length > 0, 'some players listed');
  assert.ok(teams.every(t => t === team), teams.join(','));
});

// Setup.
When(/^Kris moves "([^"]*)" to the top of the pool teams and saves$/, async function (team) {
  await this.page.click('#tab-setup');
  let i = (await state()).managers.indexOf(team);
  for (; i > 0; i--) await this.page.click(`#s-mgrlist button[data-up="${i}"]`);
  await this.page.click('#s-save');
  await eventually(async () => (await this.page.textContent('#s-msg')) === 'Saved', 'Saved');
});
Then(/^the pool teams start with "([^"]*)"$/, async function (team) { assert.equal((await state()).managers[0], team); });
Then(/^"([^"]*)" is still owned by "([^"]*)"$/, async function (name, team) {
  const s = await state();
  assert.equal(s.managers[s.picks.find(k => k.playerId === s.players.find(p => p.name === name).id).manager], team);
});
When(/^Kris presses remove next to "([^"]*)" in Setup$/, async function (team) {
  await this.page.click('#tab-setup');
  this.bag.team = team;
  await this.page.click(`#s-mgrlist button[data-del="${(await state()).managers.indexOf(team)}"]`);
});
Then(/^"Remove ([^"]*)\?" appears beside it with Yes and Cancel$/, async function (team) {
  const c = this.page.locator('#s-mgrlist .confirm');
  assert.ok((await c.textContent()).includes(`Remove ${team}?`));
  assert.ok(await c.locator('[data-delyes]').isVisible() && await c.locator('[data-delno]').isVisible());
});
Then(/^"([^"]*)" is still listed$/, async function (team) {
  const vals = await this.page.locator('#s-mgrlist input').evaluateAll(is => is.map(i => i.value));
  assert.ok(vals.includes(team));
});

When('Kris opens Setup', async function () { await this.page.click('#tab-setup'); });
Then('the most recent automatic backups are listed for download', async function () {
  await eventually(async () => (await this.page.locator('#s-backups a').count()) > 0, 'backup links');
  const href = await this.page.locator('#s-backups a').first().getAttribute('href');
  const r = await this.page.request.get(new URL(href, base + '/').href);
  assert.equal(r.status(), 200);
  assert.ok((await r.json()).picks.length > 0, 'the latest backup has the pick');
});

When(/^Kris ticks Drafted on the top (\d+) players in the list one after another, choosing an owner for each$/, async function (n) {
  this.bag.rapid = [];
  for (let i = 0; i < n; i++) {
    const row = this.page.locator('#players tr[data-rid]').first();
    const id = +(await row.getAttribute('data-rid')).split('-')[1];
    await row.locator('input.gone').click();
    const owner = this.page.locator(`#players [data-pd="${id}"] [data-pf=mgr]`);
    await owner.waitFor();
    await owner.selectOption({ index: 1 + (i % 3) });
    await this.page.locator(`#players [data-pd="${id}"] [data-act=pskip]`).click();
    this.bag.rapid.push({ id, manager: i % 3 });
  }
});
Then(/^all (\d+) are drafted with the owners chosen$/, async function (n) {
  await eventually(async () => {
    const picks = (await state()).picks;
    return this.bag.rapid.length === n && this.bag.rapid.every(r => picks.some(k => k.playerId === r.id && k.manager === r.manager));
  }, 'all rapid picks to save');
});
Then('the page shows no errors', async function () {
  assert.deepEqual(this.errors, []);
});

import { Given, When, Then } from '@cucumber/cucumber';
import assert from 'node:assert/strict';
import { base, ADMIN_PW, GUEST_PHRASE } from './world.mjs';

When('a visitor opens the site', async function () { await this.open('/'); });
Given('a visitor is on the sign-in page', async function () { await this.open('/login'); });
Then('they see the sign-in page', async function () {
  await this.page.waitForURL(base + '/login');
  assert.ok(await this.page.isVisible('#guest-btn'));
});
When('they press "Sign in as guest"', async function () { await this.page.click('#guest-btn'); });
When('they press Cancel', async function () { await this.page.click('#guest-cancel'); });
Then(/^the entry phrase field is (shown|hidden)$/, async function (how) {
  assert.equal(await this.page.isVisible('input[name=phrase]'), how === 'shown');
  assert.equal(await this.page.getAttribute('#guest-btn', 'aria-expanded'), String(how === 'shown'));
});
async function adminForm(w, name, pw) {
  await w.page.locator('input[name=name]').last().fill(name);
  await w.page.fill('input[name=password]', pw);
  await w.page.click('button.primary:text-is("Sign in")');
}
When('they sign in as Kris', async function () { await adminForm(this, 'kris', ADMIN_PW); });
When(/^they sign in as "([^"]*)" with password "([^"]*)"$/, async function (name, pw) { await adminForm(this, name, pw); });
When(/^they enter as a guest named "([^"]*)"$/, async function (name) {
  await this.page.click('#guest-btn');
  await this.page.fill('input[name=phrase]', GUEST_PHRASE);
  await this.page.locator('input[name=name]').first().fill(name);
  await this.page.click('button.primary:text-is("Enter as guest")');
});
Then(/^they see the board as "([^"]*)"$/, async function (who) {
  await this.page.waitForURL(base + '/');
  await this.page.waitForSelector('#players tbody tr');
  assert.ok((await this.page.textContent('#you')).includes(who));
});
When('they press "Sign out"', async function () { await this.page.click('a.signout'); });
Then('there is no Best buys, Drafted column, Value column, Setup tab or ranking weights', async function () {
  for (const sel of ['#recs', '#players thead th:text-is("Drafted")', '#players thead th:text-is("Value")', '#tab-setup', '#weights']) {
    assert.equal(await this.page.isVisible(sel), false, `${sel} should be hidden`);
  }
});
Then(/^they see "([^"]*)" on the sign-in page$/, async function (text) {
  assert.ok(this.page.url().endsWith('/login'));
  assert.ok((await this.page.textContent('body')).includes(text));
});

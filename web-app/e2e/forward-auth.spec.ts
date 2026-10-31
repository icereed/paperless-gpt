import { Browser, expect, Page, test } from '@playwright/test';
import { getApiToken, PORTS, setupTestEnvironment, TestEnvironment } from './test-environment';

// PAPERLESS_FORWARD_AUTH=superuser against a real paperless-ngx: paperless-gpt
// forwards the browser's paperless-ngx session to paperless-ngx and only lets
// superusers in. Both run on localhost; cookies ignore the port, so the
// paperless-ngx session reaches paperless-gpt like in a sub-path setup.
// No LLM call is made, so this runs in mock and real-LLM mode alike.

const admin = { username: 'admin', password: 'admin' }; // superuser
const plain = { username: 'e2e-plain', password: 'e2e-plain-Pw-4711' };

test.describe.configure({ mode: 'serial' });

let testEnv: TestEnvironment;
let paperlessUrl: string;
let gptUrl: string;

test.beforeAll(async () => {
  testEnv = await setupTestEnvironment({ forwardAuth: 'superuser' });
  paperlessUrl = `http://localhost:${testEnv.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
  gptUrl = `http://localhost:${testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;

  const res = await fetch(`${paperlessUrl}/api/users/`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: 'Basic ' + btoa(`${admin.username}:${admin.password}`),
    },
    body: JSON.stringify({ ...plain, is_active: true, is_superuser: false }),
  });
  expect(res.status, await res.clone().text()).toBe(201);
});

test.afterAll(async () => {
  await testEnv?.cleanup();
});

async function signInToPaperless(page: Page, user: { username: string; password: string }) {
  await page.goto(`${paperlessUrl}/accounts/login/`);
  await page.locator('input[name="login"]').fill(user.username);
  await page.locator('input[name="password"]').fill(user.password);
  await page.locator('button[type="submit"]').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/accounts/login'));
}

async function signedInPage(browser: Browser, user: { username: string; password: string }) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await signInToPaperless(page, user);
  return page;
}

test('anonymous visitors are sent to the paperless-ngx login', async ({ page }) => {
  await page.goto(`${gptUrl}/history`);
  await expect(page).toHaveURL(new RegExp(`^${paperlessUrl}/accounts/login/\\?next=%2Fhistory`));

  const api = await page.request.get(`${gptUrl}/api/version`);
  expect(api.status()).toBe(401);
  expect(await api.json()).toMatchObject({ login_url: `${paperlessUrl}/accounts/login/` });
});

test('a paperless-ngx superuser gets in with the paperless-ngx session', async ({ browser }) => {
  const page = await signedInPage(browser, admin);
  const response = await page.goto(`${gptUrl}/`);
  expect(response?.status()).toBe(200);
  expect(response?.headers()['cache-control']).toBe('no-store');
  await expect(page.locator('#root')).not.toBeEmpty();

  const api = await page.request.get(`${gptUrl}/api/version`);
  expect(api.status()).toBe(200);
  await page.context().close();
});

test('a signed-in account without superuser status is denied', async ({ browser }) => {
  const page = await signedInPage(browser, plain);
  const response = await page.goto(`${gptUrl}/`);
  expect(response?.status()).toBe(403);
  // A fresh account lacks paperless-ngx' view_uisettings permission, so this
  // also covers the /api/profile/ fallback (which reports no username).
  await expect(page.getByRole('heading', { name: 'Access denied' })).toBeVisible();
  await expect(page.getByText('superuser status')).toBeVisible();

  const api = await page.request.get(`${gptUrl}/api/version`);
  expect(api.status()).toBe(403);
  await page.context().close();
});

test('API clients can use a paperless-ngx token instead of a session', async ({ request }) => {
  const token = await getApiToken(paperlessUrl, admin);
  const ok = await request.get(`${gptUrl}/api/version`, { headers: { Authorization: `Token ${token}` } });
  expect(ok.status()).toBe(200);

  const wrong = await request.get(`${gptUrl}/api/version`, { headers: { Authorization: 'Token not-a-token' } });
  expect(wrong.status()).toBe(401);
});

test('writes from another site are rejected even with a valid session', async ({ browser }) => {
  const page = await signedInPage(browser, admin);
  const crossSite = await page.request.post(`${gptUrl}/api/prompts`, {
    headers: { Origin: 'https://evil.example' },
    data: {},
  });
  expect(crossSite.status()).toBe(403);
  expect(await crossSite.json()).toMatchObject({ error: 'cross-origin request rejected' });

  // The same request from paperless-gpt's own origin reaches the handler,
  // which rejects the empty prompt update itself.
  const sameOrigin = await page.request.post(`${gptUrl}/api/prompts`, {
    headers: { Origin: gptUrl },
    data: {},
  });
  expect(sameOrigin.status()).toBe(400);
  await page.context().close();
});

test('the web app returns to the login once the session is gone', async ({ browser }) => {
  const page = await signedInPage(browser, admin);
  await page.goto(`${gptUrl}/history`);
  await expect(page).toHaveURL(`${gptUrl}/history`);

  await page.context().clearCookies();
  // Any API call of the app now gets a 401 and sends the browser to the login.
  await page.evaluate(() => fetch('./api/version'));
  await expect(page).toHaveURL(new RegExp(`^${paperlessUrl}/accounts/login/\\?next=%2Fhistory`));
  await page.context().close();
});

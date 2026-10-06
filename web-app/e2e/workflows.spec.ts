import { expect, test } from '@playwright/test';
import path, { dirname } from 'path';
import { fileURLToPath } from 'url';
import {
  addTagToDocument,
  getTagByName,
  isMockLlmMode,
  PORTS,
  setupTestEnvironment,
  TestEnvironment,
  uploadDocument,
} from './test-environment';

const __dirname = dirname(fileURLToPath(import.meta.url));
const credentials = { username: 'admin', password: 'admin' };
const auth = { Authorization: 'Basic ' + btoa(`${credentials.username}:${credentials.password}`) };

// The workflow's title prompt carries a marker that only the
// mocks/workflow-title.json stub answers, so seeing its title proves the
// workflow's own prompt was used rather than the global one.
const WORKFLOW_PROMPT = 'E2E-WORKFLOW-MARKER: write a short title for this document.\n{{.Content}}';
const WORKFLOW_TITLE = 'Workflow Suggested Title';

test.skip(!isMockLlmMode(), 'needs the deterministic mock LLM (npm run test:e2e:mock)');

let testEnv: TestEnvironment;

test.beforeAll(async () => {
  testEnv = await setupTestEnvironment();
});

test.afterAll(async () => {
  await testEnv?.cleanup();
});

test('an AI workflow set up in the UI processes documents with its own prompt', async ({ page }) => {
  const paperlessUrl = `http://localhost:${testEnv.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
  const gptUrl = `http://localhost:${testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;

  const { id: documentId } = await uploadDocument(
    paperlessUrl,
    path.join(__dirname, 'fixtures', 'test-document.txt'),
    'Workflow Original Title',
    credentials
  );

  // 1. Create the workflow in the UI.
  await page.goto(`${gptUrl}/workflows`);
  await page.getByRole('button', { name: 'New workflow' }).first().click();
  await page.getByLabel('Name').fill('E2E Invoices');
  await page.getByLabel(/Trigger tag/).fill('e2e-invoices');
  await page.getByLabel('Completion tag').fill('e2e-invoices-done');
  for (const step of ['Tags', 'Correspondent', 'Document type', 'Created date']) {
    await page.getByRole('radiogroup', { name: step }).getByRole('radio', { name: 'Off', exact: true }).click();
  }
  await page.getByRole('radiogroup', { name: 'Title' }).getByRole('radio', { name: 'On', exact: true }).click();
  // Steps that are off don't show a prompt.
  await expect(page.getByRole('tab', { name: /^Title/ })).toBeVisible();
  for (const hidden of [/^Tags/, /^Correspondent/, /^Document type/, /^Created date/]) {
    await expect(page.getByRole('tab', { name: hidden })).toHaveCount(0);
  }
  // The global prompt is shown read-only; an own version has to be asked for.
  await expect(page.getByLabel('Global Title prompt (read-only)')).toBeVisible();
  await page.getByLabel('Global Title prompt (read-only)').scrollIntoViewIfNeeded();
  await page.screenshot({ path: 'test-results/workflow-global-prompt.png' });
  await page.getByRole('button', { name: 'Write own prompt for this workflow' }).click();
  await expect(page.getByText('Own title prompt for this workflow')).toBeVisible();
  await page.getByLabel('Title prompt', { exact: true }).fill(WORKFLOW_PROMPT);

  // 2. Test it on the document before saving: nothing may be written.
  await page.getByRole('button', { name: 'Test on a document' }).click();
  await page.getByPlaceholder(/Document ID/).fill(String(documentId));
  await page.getByRole('button', { name: 'Run test' }).click();
  await expect(page.getByText(WORKFLOW_TITLE)).toBeVisible({ timeout: 60_000 });
  await page.screenshot({ path: 'test-results/workflow-test-run.png' });
  const untouched = await (await fetch(`${paperlessUrl}/api/documents/${documentId}/`, { headers: auth })).json();
  expect(untouched.title).toBe('Workflow Original Title');

  // 3. Save. The tags are created in paperless-ngx right away.
  await page.getByRole('button', { name: 'Create workflow' }).click();
  await expect(page.getByText('Workflow created.')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'E2E Invoices' })).toBeVisible();
  const triggerId = await getTagByName(paperlessUrl, 'e2e-invoices', credentials);
  const doneId = await getTagByName(paperlessUrl, 'e2e-invoices-done', credentials);
  expect(triggerId).not.toBeNull();
  expect(doneId).not.toBeNull();

  // 4. Tag the document; the background processor picks it up.
  await addTagToDocument(paperlessUrl, documentId, triggerId!, credentials);
  await expect
    .poll(
      async () => {
        const doc = await (await fetch(`${paperlessUrl}/api/documents/${documentId}/`, { headers: auth })).json();
        return { title: doc.title, tags: [...doc.tags].sort() };
      },
      { timeout: 90_000, intervals: [2_000] }
    )
    .toEqual({ title: WORKFLOW_TITLE, tags: [doneId] });
  await page.screenshot({ path: 'test-results/workflow-processed.png' });
});

import { expect, Page, test } from '@playwright/test';
import path, { dirname } from 'path';
import { fileURLToPath } from 'url';
import {
  addTagToDocument,
  createTag,
  getTagByName,
  isMockLlmMode,
  PORTS,
  setupTestEnvironment,
  TestEnvironment,
  uploadDocument,
} from './test-environment';

// End-to-end coverage for AI workflows, against real paperless-ngx and
// paperless-gpt containers with the deterministic mock LLM. The tests share
// one environment and run in order.

const __dirname = dirname(fileURLToPath(import.meta.url));
const credentials = { username: 'admin', password: 'admin' };
const auth = { Authorization: 'Basic ' + btoa(`${credentials.username}:${credentials.password}`) };
const fixture = path.join(__dirname, 'fixtures', 'test-document.txt');

// The workflow title prompt carries a marker that only the
// mocks/workflow-title.json stub answers, so seeing its title proves the
// workflow's own prompt was used rather than the global one.
const WORKFLOW_PROMPT = 'E2E-WORKFLOW-MARKER: write a short title for this document.\n{{.Content}}';
const WORKFLOW_TITLE = 'Workflow Suggested Title';
const GLOBAL_TITLE = 'Mock Suggested Title'; // mocks/title.json
const AUTO_TAG = 'paperless-gpt-auto';
const AUTO_TAG_COMPLETE = 'paperless-gpt-auto-complete';

test.skip(!isMockLlmMode(), 'needs the deterministic mock LLM (npm run test:e2e:mock)');
test.describe.configure({ mode: 'serial' });

let testEnv: TestEnvironment;
let paperlessUrl: string;

const gptUrl = () => `http://localhost:${testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;

test.beforeAll(async () => {
  testEnv = await setupTestEnvironment();
  paperlessUrl = `http://localhost:${testEnv.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
});

test.afterAll(async () => {
  await testEnv?.cleanup();
});

// --- helpers -----------------------------------------------------------------

interface PaperlessDoc {
  id: number;
  title: string;
  tags: number[];
  correspondent: number | null;
}

async function getDocument(id: number): Promise<PaperlessDoc> {
  const res = await fetch(`${paperlessUrl}/api/documents/${id}/`, { headers: auth });
  expect(res.ok).toBeTruthy();
  return res.json();
}

async function tagId(name: string): Promise<number> {
  const id = (await getTagByName(paperlessUrl, name, credentials)) ?? (await createTag(paperlessUrl, name, credentials));
  return id;
}

async function setTags(documentId: number, tagIds: number[]) {
  const res = await fetch(`${paperlessUrl}/api/documents/${documentId}/`, {
    method: 'PATCH',
    headers: { ...auth, 'Content-Type': 'application/json' },
    body: JSON.stringify({ tags: tagIds }),
  });
  expect(res.ok).toBeTruthy();
}

async function gptApi(method: string, path: string, body?: unknown) {
  return fetch(`${gptUrl()}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

async function listWorkflowIds(): Promise<string[]> {
  const res = await gptApi('GET', '/api/workflows');
  expect(res.ok).toBeTruthy();
  return ((await res.json()) as { id: string }[]).map((w) => w.id);
}

async function waitForProcessed(documentId: number, done: (doc: PaperlessDoc) => boolean): Promise<PaperlessDoc> {
  let last: PaperlessDoc | undefined;
  await expect
    .poll(
      async () => {
        last = await getDocument(documentId);
        return done(last);
      },
      { timeout: 90_000, intervals: [2_000] }
    )
    .toBe(true);
  return last!;
}

async function openNewWorkflow(page: Page) {
  await page.goto(`${gptUrl()}/workflows`);
  await page.getByRole('button', { name: 'New workflow' }).first().click();
}

// --- tests -------------------------------------------------------------------

test('a workflow set up in the UI is tested on a document, saved, and processes documents with its own prompt', async ({ page }) => {
  const { id: documentId } = await uploadDocument(paperlessUrl, fixture, 'Workflow Original Title', credentials);

  await openNewWorkflow(page);
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
  await page.getByRole('button', { name: 'Write own prompt for this workflow' }).click();
  await expect(page.getByText('Own title prompt for this workflow')).toBeVisible();
  await page.getByLabel('Title prompt', { exact: true }).fill(WORKFLOW_PROMPT);

  // Test it on the document before saving: nothing may be written.
  await page.getByRole('button', { name: 'Test on a document' }).click();
  await page.getByPlaceholder(/Document ID/).fill(String(documentId));
  await page.getByRole('button', { name: 'Run test' }).click();
  await expect(page.getByText(WORKFLOW_TITLE)).toBeVisible({ timeout: 60_000 });
  await page.screenshot({ path: 'test-results/workflow-test-run.png' });
  expect((await getDocument(documentId)).title).toBe('Workflow Original Title');

  // Save. The tags are created in paperless-ngx right away.
  await page.getByRole('button', { name: 'Create workflow' }).click();
  await expect(page.getByText('Workflow created.')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'E2E Invoices' })).toBeVisible();
  const triggerId = await getTagByName(paperlessUrl, 'e2e-invoices', credentials);
  const doneId = await getTagByName(paperlessUrl, 'e2e-invoices-done', credentials);
  expect(triggerId).not.toBeNull();
  expect(doneId).not.toBeNull();

  // Tag the document; the background processor picks it up.
  await addTagToDocument(paperlessUrl, documentId, triggerId!, credentials);
  const doc = await waitForProcessed(documentId, (d) => d.title === WORKFLOW_TITLE);
  expect(doc.tags).toEqual([doneId]);
  expect(doc.correspondent, 'the correspondent step is off for this workflow').toBeNull();
  await page.screenshot({ path: 'test-results/workflow-processed.png' });
});

test('a document with a workflow trigger and AUTO_TAG is processed once, by the workflow', async () => {
  const create = await gptApi('POST', '/api/workflows', {
    name: 'E2E Contracts',
    trigger_tag: 'e2e-contracts',
    completion_tag: 'e2e-contracts-done',
    generate_titles: true,
    generate_tags: false,
    generate_correspondents: false,
    generate_document_types: false,
    generate_created_date: false,
    prompts: { title_prompt: WORKFLOW_PROMPT },
  });
  expect(create.status).toBe(201);

  const { id: documentId } = await uploadDocument(paperlessUrl, fixture, 'Contract Original Title', credentials);
  const trigger = await tagId('e2e-contracts');
  const autoTag = await tagId(AUTO_TAG);
  const done = await tagId('e2e-contracts-done');
  await setTags(documentId, [trigger, autoTag]);

  const doc = await waitForProcessed(documentId, (d) => d.tags.includes(done));
  expect(doc.title, 'the workflow prompt was used, not the global one').toBe(WORKFLOW_TITLE);
  expect(doc.tags, 'both triggers are removed; the workflow completion tag replaces AUTO_TAG_COMPLETE').toEqual([done]);
  expect(doc.correspondent).toBeNull();
});

test('a document with only AUTO_TAG still goes through the default processing', async () => {
  const { id: documentId } = await uploadDocument(paperlessUrl, fixture, 'Default Original Title', credentials);
  const autoTag = await tagId(AUTO_TAG);
  await setTags(documentId, [autoTag]);

  const complete = await tagId(AUTO_TAG_COMPLETE);
  const doc = await waitForProcessed(documentId, (d) => d.tags.includes(complete));
  expect(doc.title, 'the global title prompt was used').toBe(GLOBAL_TITLE);
  expect(doc.tags).not.toContain(autoTag);
  expect(doc.correspondent, 'the default processing sets a correspondent').not.toBeNull();
});

test('workflows that would loop are refused, in the UI and by the API', async ({ page }) => {
  await openNewWorkflow(page);
  await page.getByLabel(/Trigger tag/).fill('Paperless-GPT-Auto');
  await expect(page.getByText('This is AUTO_TAG, which already runs the default processing.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create workflow' })).toBeDisabled();

  await page.getByLabel(/Trigger tag/).fill('e2e-loop');
  await page.getByLabel('Completion tag').fill('E2E-Loop');
  await expect(page.getByText(/Must differ from the trigger tag/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create workflow' })).toBeDisabled();

  const sameTags = await gptApi('POST', '/api/workflows', { name: 'Loop', trigger_tag: 'e2e-loop', completion_tag: 'e2e-loop' });
  expect(sameTags.status).toBe(400);
  const chained = await gptApi('POST', '/api/workflows', { name: 'Chain', trigger_tag: 'e2e-chain', completion_tag: 'e2e-invoices' });
  expect(chained.status).toBe(409);
  const reserved = await gptApi('POST', '/api/workflows', { name: 'Reserved', trigger_tag: AUTO_TAG_COMPLETE });
  expect(reserved.status).toBe(400);
  expect(await listWorkflowIds()).not.toContain('e2e-loop');
});

test('workflow files written by hand are picked up, old settings are migrated, and everything survives a restart', async () => {
  // A workflow written as files, the way someone keeping them in git would.
  await testEnv.paperlessGpt.copyContentToContainer([
    {
      content: JSON.stringify({ version: 1, name: 'E2E Hand Made', trigger_tag: 'e2e-hand', completion_tag: 'e2e-hand-done', generate_tags: false }),
      target: '/app/prompts/workflows/e2e-hand/workflow.json',
      mode: 0o644,
    },
    { content: WORKFLOW_PROMPT, target: '/app/prompts/workflows/e2e-hand/title_prompt.tmpl', mode: 0o644 },
  ]);
  await expect.poll(listWorkflowIds, { timeout: 30_000 }).toContain('e2e-hand');
  // Its tags are created in paperless-ngx without a restart, too.
  await expect.poll(() => getTagByName(paperlessUrl, 'e2e-hand-done', credentials), { timeout: 30_000 }).not.toBeNull();

  // A workflow in config/settings.json, where builds before #1110 kept them.
  await testEnv.paperlessGpt.copyContentToContainer([
    {
      content: JSON.stringify({
        custom_fields_enable: false,
        custom_fields_selected_ids: [],
        custom_fields_write_mode: 'append',
        workflows: [{ id: 'e2e-legacy', name: 'E2E Legacy', trigger_tag: 'e2e-legacy', prompts: { title_prompt: WORKFLOW_PROMPT } }],
      }),
      target: '/app/config/settings.json',
      mode: 0o666,
    },
  ]);

  const before = await listWorkflowIds();
  await testEnv.paperlessGpt.restart();
  await expect.poll(async () => (await gptApi('GET', '/api/workflows').catch(() => null))?.ok ?? false, { timeout: 60_000 }).toBe(true);

  const after = await listWorkflowIds();
  for (const id of before) {
    expect(after, `workflow ${id} survives the restart`).toContain(id);
  }
  expect(after, 'the workflow from settings.json is migrated').toContain('e2e-legacy');

  const legacy = await (await gptApi('GET', '/api/workflows')).json();
  const migrated = legacy.find((w: { id: string }) => w.id === 'e2e-legacy');
  expect(migrated.prompts.title_prompt).toBe(WORKFLOW_PROMPT);
});

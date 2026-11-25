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

test('a workflow fills only its own custom fields, not the ones selected in Settings', async ({ page }) => {
  // Two custom fields in paperless-ngx; Settings selects only "Invoice number".
  const createField = async (name: string, dataType: string) => {
    const res = await fetch(`${paperlessUrl}/api/custom_fields/`, {
      method: 'POST',
      headers: { ...auth, 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, data_type: dataType }),
    });
    expect(res.ok).toBeTruthy();
    return ((await res.json()) as { id: number }).id;
  };
  const grossPay = await createField('Gross pay', 'monetary');
  const invoiceNumber = await createField('Invoice number', 'string');

  await gptApi('GET', '/api/custom_fields?force_pull=true');
  const current = (await (await gptApi('GET', '/api/settings')).json()).settings;
  const saved = await gptApi('POST', '/api/settings', {
    ...current,
    custom_fields_enable: true,
    custom_fields_selected_ids: [invoiceNumber],
    custom_fields_write_mode: 'append',
  });
  expect(saved.ok).toBeTruthy();
  await expect
    .poll(async () => ((await (await gptApi('GET', '/api/workflows/defaults')).json()).custom_fields as { name: string }[]).map((f) => f.name), { timeout: 30_000 })
    .toContain('Gross pay');

  // A payslip workflow that only fills "Gross pay".
  await openNewWorkflow(page);
  await page.getByLabel('Name').fill('E2E Payslips');
  await page.getByLabel(/Trigger tag/).fill('e2e-payslips');
  for (const step of ['Title', 'Tags', 'Correspondent', 'Document type', 'Created date']) {
    await page.getByRole('radiogroup', { name: step }).getByRole('radio', { name: 'Off', exact: true }).click();
  }
  await page.getByRole('radiogroup', { name: 'Custom fields', exact: true }).getByRole('radio', { name: 'On', exact: true }).click();
  const picker = page.getByRole('radiogroup', { name: 'Custom fields to fill' });
  await expect(picker.getByText('Invoice number'), 'the Settings selection is shown').toBeVisible();
  await picker.getByLabel('Only these fields for this workflow').check();
  await picker.getByLabel('Gross pay').check();
  // Its own custom field prompt carries the marker the mock answers.
  await page.getByRole('tab', { name: /^Custom fields/ }).click();
  await page.getByRole('button', { name: 'Write own prompt for this workflow' }).click();
  await page.getByLabel('Custom fields prompt', { exact: true }).fill('E2E-PAYSLIP-MARKER: extract these fields.\n{{ .CustomFieldsXML }}\n{{ .Content }}');
  await page.getByRole('button', { name: 'Create workflow' }).click();
  await expect(page.getByText('Workflow created.')).toBeVisible();
  await expect(page.getByText('Fills: Gross pay')).toBeVisible();

  // The mock only answers a prompt that asks for "Gross pay" and not for
  // "Invoice number", so a filled field proves the workflow's selection was used.
  const { id: documentId } = await uploadDocument(paperlessUrl, fixture, 'Payslip September', credentials);
  await setTags(documentId, [await tagId('e2e-payslips')]);
  type WithFields = PaperlessDoc & { custom_fields: { field: number; value: string }[] };
  const doc = (await waitForProcessed(documentId, (d) => (d as WithFields).custom_fields.length > 0)) as WithFields;
  expect(doc.custom_fields).toEqual([{ field: grossPay, value: 'EUR4200.00' }]);
});

// --- approval ------------------------------------------------------------------
//
// Someone who doesn't trust full automation yet: a mail rule tags invoices,
// the workflow prepares suggestions, and nothing lands in paperless-ngx until
// they have looked at it, corrected it and applied it.

const APPROVAL_WORKFLOW = 'E2E Approved Invoices';
const APPROVAL_TRIGGER = 'e2e-approve';
const APPROVAL_DONE = 'e2e-approve-done';
const APPROVAL_TITLE = 'Approval Suggested Title'; // mocks/workflow-approval-title.json
const REVIEW_TAG = 'paperless-gpt'; // MANUAL_TAG

interface PendingReview {
  id: number;
  workflow_name: string;
  created_at: string;
}

async function pendingReviews(): Promise<PendingReview[]> {
  const res = await gptApi('GET', '/api/reviews');
  expect(res.ok).toBeTruthy();
  return res.json();
}

// Waits until the workflow has parked the document for approval.
async function waitForApproval(documentId: number) {
  const reviewTag = await tagId(REVIEW_TAG);
  return waitForProcessed(documentId, (d) => d.tags.includes(reviewTag));
}

test('a workflow with approval holds its suggestions until the user corrects and applies them', async ({ page }) => {
  // Set up: title and correspondent, with approval.
  await openNewWorkflow(page);
  await page.getByLabel('Name').fill(APPROVAL_WORKFLOW);
  await page.getByLabel(/Trigger tag/).fill(APPROVAL_TRIGGER);
  await page.getByLabel('Completion tag').fill(APPROVAL_DONE);
  for (const step of ['Tags', 'Document type', 'Created date', 'Custom fields']) {
    await page.getByRole('radiogroup', { name: step, exact: true }).getByRole('radio', { name: 'Off', exact: true }).click();
  }
  for (const step of ['Title', 'Correspondent']) {
    await page.getByRole('radiogroup', { name: step, exact: true }).getByRole('radio', { name: 'On', exact: true }).click();
  }
  await page.getByRole('tab', { name: /^Title/ }).click();
  await page.getByRole('button', { name: 'Write own prompt for this workflow' }).click();
  await page.getByLabel('Title prompt', { exact: true }).fill('E2E-APPROVAL-MARKER: title this invoice.\n{{.Content}}');
  await page.getByLabel('Wait for my approval before writing to paperless-ngx').check();
  await page.getByRole('button', { name: 'Create workflow' }).click();
  await expect(page.getByText('Workflow created.')).toBeVisible();
  await expect(page.getByText('Needs approval')).toBeVisible();

  // An invoice arrives; a mail rule would add the trigger tag.
  const mine = await tagId('e2e-mine');
  const { id: documentId } = await uploadDocument(paperlessUrl, fixture, 'Approval Original', credentials);
  await setTags(documentId, [await tagId(APPROVAL_TRIGGER), mine]);

  // Nothing but the tag handover is written: the trigger is replaced by the
  // review tag, so the document visibly waits and isn't picked up again.
  const parked = await waitForApproval(documentId);
  expect(parked.title).toBe('Approval Original');
  expect(parked.correspondent).toBeNull();
  expect(parked.tags).not.toContain(await tagId(APPROVAL_TRIGGER));
  expect(parked.tags, 'the completion tag only comes with the approval').not.toContain(await tagId(APPROVAL_DONE));
  expect(parked.tags).toContain(mine);

  // It is generated once: a later poll must not regenerate it.
  const [first] = (await pendingReviews()).filter((r) => r.id === documentId);
  expect(first.workflow_name).toBe(APPROVAL_WORKFLOW);
  await page.waitForTimeout(15_000); // more than one poll cycle
  const [again] = (await pendingReviews()).filter((r) => r.id === documentId);
  expect(again.created_at).toBe(first.created_at);

  // Home tells them something is waiting, also after a reload.
  await page.goto(`${gptUrl()}/`);
  const queue = page.getByRole('region', { name: 'Waiting for your approval' });
  await expect(queue).toContainText(APPROVAL_WORKFLOW);
  await page.reload();
  await expect(queue).toBeVisible();
  await queue.getByRole('button', { name: /^Review/ }).click();

  // The suggestion says where it came from; they fix the title and keep the
  // correspondent as it is.
  const card = page.getByRole('article', { name: 'Suggestions for “Approval Original”' });
  await expect(card.getByText(`From workflow: ${APPROVAL_WORKFLOW}`)).toBeVisible();
  await expect(card.getByLabel('Suggested title', { exact: true })).toHaveValue(APPROVAL_TITLE);
  await card.getByLabel('Suggested title', { exact: true }).fill('Invoice ACME 2026-10');
  await card.getByLabel('Apply suggested correspondent').uncheck();
  await card.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByText(/Applied \d+ field change/)).toBeVisible();

  // In paperless-ngx: exactly what they approved, and the tags handed over.
  const doc = await waitForProcessed(documentId, (d) => d.title === 'Invoice ACME 2026-10');
  expect(doc.correspondent, 'the unticked correspondent is not written').toBeNull();
  expect(doc.tags.sort()).toEqual([await tagId(APPROVAL_DONE), mine].sort());
  expect((await pendingReviews()).map((r) => r.id)).not.toContain(documentId);
  await expect(queue).toHaveCount(0);
});

test('a skipped approval stays waiting, a discarded one is released untouched, and switching approval off writes directly', async ({ page }) => {
  const trigger = await tagId(APPROVAL_TRIGGER);
  const done = await tagId(APPROVAL_DONE);
  const reviewTag = await tagId(REVIEW_TAG);
  const { id: later } = await uploadDocument(paperlessUrl, fixture, 'Approval Later', credentials);
  const { id: mistake } = await uploadDocument(paperlessUrl, fixture, 'Approval Newsletter', credentials);
  await setTags(later, [trigger]);
  await setTags(mistake, [trigger]);
  await waitForApproval(later);
  await waitForApproval(mistake);

  await page.goto(`${gptUrl()}/`);
  await page.getByRole('region', { name: 'Waiting for your approval' }).getByRole('button', { name: 'Review 2' }).click();

  // Routed by mistake: discard.
  const mistakeCard = page.getByRole('article', { name: 'Suggestions for “Approval Newsletter”' });
  await mistakeCard.getByRole('button', { name: 'Discard' }).click();
  await expect(mistakeCard.getByText('Discarded — nothing was written')).toBeVisible();
  // Not now: skip. That decides the last card and closes the review.
  const laterCard = page.getByRole('article', { name: 'Suggestions for “Approval Later”' });
  await laterCard.getByRole('button', { name: 'Skip' }).click();

  // Back on Home, only the skipped one is still waiting.
  await expect(page.getByRole('region', { name: 'Waiting for your approval' }).getByRole('button', { name: 'Review 1' })).toBeVisible();
  const waiting = (await pendingReviews()).map((r) => r.id);
  expect(waiting).toContain(later);
  expect(waiting).not.toContain(mistake);

  const discarded = await getDocument(mistake);
  expect(discarded.title).toBe('Approval Newsletter');
  expect(discarded.tags, 'released: no review tag, no trigger, no completion tag').toEqual([]);
  expect((await getDocument(later)).tags).toEqual([reviewTag]);

  // They trust it now: approval off. New documents are written directly; the
  // one still waiting is not applied behind their back.
  await page.goto(`${gptUrl()}/workflows`);
  await page.getByRole('button', { name: `Edit ${APPROVAL_WORKFLOW}` }).click();
  await page.getByLabel('Wait for my approval before writing to paperless-ngx').uncheck();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Workflow saved.')).toBeVisible();
  await expect(page.getByText('Needs approval')).toHaveCount(0);

  const { id: direct } = await uploadDocument(paperlessUrl, fixture, 'Approval Direct', credentials);
  await setTags(direct, [trigger]);
  const doc = await waitForProcessed(direct, (d) => d.title === APPROVAL_TITLE);
  expect(doc.tags).toEqual([done]);
  expect((await pendingReviews()).map((r) => r.id)).toContain(later);
  expect((await getDocument(later)).title).toBe('Approval Later');
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

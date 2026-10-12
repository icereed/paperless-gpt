import { expect, test } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import {
  addTagToDocument, getTagByName, isMockLlmMode, PORTS,
  setupTestEnvironment, TestEnvironment, uploadDocument,
} from './test-environment';

const credentials = { username: 'admin', password: 'admin' };
const auth = { Authorization: 'Basic ' + btoa(`${credentials.username}:${credentials.password}`) };
test.skip(!isMockLlmMode(), 'requires WireMock');
let environment: TestEnvironment;


// Each two-page pair is a separate synthetic letter. No private document fixtures.
function batchPDF(label = '', pages = 6): Buffer {
  const objects = ['<< /Type /Catalog /Pages 2 0 R >>', '', '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'];
  const kids: string[] = [];
  for (let page = 1; page <= pages; page++) {
    const id = objects.length + 1;
    kids.push(`${id} 0 R`);
    const content = `BT /F1 18 Tf 50 750 Td (Sender ${Math.ceil(page / 2)} - Page ${(page - 1) % 2 + 1} of 2 ${label}) Tj ET`;
    objects.push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents ${id + 1} 0 R >>`,
      `<< /Length ${content.length} >>\nstream\n${content}\nendstream`);
  }
  objects[1] = `<< /Type /Pages /Kids [${kids.join(' ')}] /Count ${pages} >>`;
  let pdf = '%PDF-1.4\n';
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(pdf.length);
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = pdf.length;
  pdf += `xref\n0 ${offsets.length}\n0000000000 65535 f \n`;
  offsets.slice(1).forEach(offset => { pdf += `${String(offset).padStart(10, '0')} 00000 n \n`; });
  pdf += `trailer\n<< /Size ${offsets.length} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf, 'ascii');
}

test.describe('dry-run Review', () => {
test.beforeAll(async () => { environment = await setupTestEnvironment({ splitAuto: true }); });
test.afterAll(async () => { await environment?.cleanup(); });

test('a three-document batch produces a persisted dry-run Suggestion without splitting', async ({ request, page }, info) => {
  const ngx = `http://localhost:${environment.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
  const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
  await mkdir(info.outputDir, { recursive: true });
  const fixture = info.outputPath('synthetic-batch.pdf');
  await writeFile(fixture, batchPDF());
  const document = await uploadDocument(ngx, fixture, 'Synthetic batch', credentials);
  const splitTag = await getTagByName(ngx, 'paperless-gpt-split', credentials);
  expect(splitTag).not.toBeNull();
  await addTagToDocument(ngx, document.id, splitTag!, credentials);
  await expect.poll(async () => {
    const response = await request.get(`${gpt}/api/split-suggestions`);
    expect(response.ok()).toBeTruthy();
    const data = await response.json();
    return data.results.find((s: { document_id: number }) => s.document_id === document.id)?.status;
  }, { timeout: 90_000, intervals: [1000] }).toBe('review');
  const data = await (await request.get(`${gpt}/api/split-suggestions`)).json();
  expect(data.config.dry_run).toBe(true);
  expect(data.config.auto).toBe(true);
  const suggestion = data.results.find((s: { document_id: number }) => s.document_id === document.id);
  expect(suggestion.page_count).toBe(6);
  expect(suggestion.boundaries.map((b: { after_page: number }) => b.after_page)).toEqual([2, 4]);
  const apply = await request.post(`${gpt}/api/split-suggestions/${suggestion.id}/apply`, { data: { boundaries: [2, 4] } });
  expect(apply.status()).toBe(409);
  expect((await apply.json()).error).toContain('SPLIT_DRY_RUN');
  const originals = await (await request.get(`${ngx}/api/documents/`, { headers: auth })).json();
  expect(originals.count).toBe(1);
  expect(originals.results[0].tags).toContain(splitTag);

  await page.goto(`${gpt}/splitting`);
  await expect(page.getByRole('heading', { name: 'Split Suggestions', exact: true })).toBeVisible();
  await page.getByRole('button', { name: /Review Synthetic batch/ }).click();
  await expect(page.getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
  const boundary = page.getByRole('button', { name: 'Split after page 2', exact: true });
  await expect(boundary).toHaveAttribute('aria-pressed', 'true');
  await boundary.click();
  await expect(boundary).toHaveAttribute('aria-pressed', 'false');
  await expect(page.getByText('2 documents: 1-4, 5-6', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Skip', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Skipped');
  const skippedData = await (await request.get(`${gpt}/api/split-suggestions`)).json();
  expect(skippedData.results[0].status).toBe('skipped');
  await page.getByRole('button', { name: /Review Synthetic batch/ }).click();
  await expect(page.getByRole('button', { name: 'Split after page 2', exact: true })).toHaveAttribute('aria-pressed', 'true');
});

test('a lost Apply response requires a refresh and keeps an uncertain split disabled', async ({ page }) => {
  const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
  let state = 'review';
  let submissions = 0;
  await page.route('**/api/split-suggestions?*', route => route.fulfill({ json: {
    count: 1, config: { dry_run: false, delete_originals: false, tag: 'paperless-gpt-split' },
    results: [{ id: 900, document_id: 900, document_title: 'Uncertain batch', page_count: 3, status: state,
      boundaries: [{ after_page: 1, confidence: 0.95, reason: 'New letter' }], updated_at: state }],
  } }));
  await page.route('**/api/documents/900/pages/*/image?*', route => route.fulfill({ status: 503 }));
  await page.route('**/api/split-suggestions/900/apply', route => {
    submissions++;
    state = 'uncertain';
    return route.abort('connectionreset');
  });
  await page.goto(`${gpt}/splitting`);
  await page.getByRole('button', { name: 'Review Uncertain batch' }).click();
  await expect(page.getByText('Preview unavailable for page 1')).toBeVisible();
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Apply split' }).click();
  await expect(page.getByRole('alert')).toContainText('Refresh');
  await expect(page.getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await page.getByRole('button', { name: 'Review Uncertain batch' }).click();
  await expect(page.getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
  await expect(page.getByText('Check the documents and task log in paperless-ngx before splitting this batch again.')).toBeVisible();
  expect(submissions).toBe(1);
});

test('the Suggestion list can recover from a load error and supports pagination', async ({ page }) => {
  const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
  let fail = true;
  await page.route('**/api/split-suggestions?*', route => {
    if (fail) { fail = false; return route.fulfill({ status: 503, json: { error: 'Temporarily unavailable' } }); }
    const secondPage = new URL(route.request().url()).searchParams.get('page') === '2';
    return route.fulfill({ json: {
      count: 51, config: { dry_run: true, delete_originals: false, tag: 'paperless-gpt-split' },
      results: [{ id: secondPage ? 901 : 900, document_id: 900, document_title: secondPage ? 'Older batch' : 'Recent batch', page_count: 105,
        status: 'review', boundaries: [], updated_at: 'v1' }],
    } });
  });
  await page.route('**/api/documents/900/pages/*/image?*', route => route.fulfill({ status: 503 }));
  await page.goto(`${gpt}/splitting`);
  await expect(page.getByRole('alert')).toContainText('Temporarily unavailable');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Review Older batch' })).toBeVisible();
  await page.getByRole('button', { name: 'Review Older batch' }).click();
  await expect(page.getByRole('button', { name: /^Split after page/ })).toHaveCount(104);
  await expect(page.getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Previous', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Review Recent batch' })).toBeVisible();
});
});

test.describe('Apply split', () => {
  test.beforeAll(async () => { environment = await setupTestEnvironment({ splitDryRun: false }); });
  test.afterAll(async () => { await environment?.cleanup(); });

  test('Review applies edited boundaries and children inherit AUTO_TAG without SPLIT_TAG', async ({ page, request }, info) => {
    const ngx = `http://localhost:${environment.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
    const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
    await mkdir(info.outputDir, { recursive: true });
    const fixture = info.outputPath('synthetic-batch.pdf');
    await writeFile(fixture, batchPDF());
    const original = await uploadDocument(ngx, fixture, 'Apply batch', credentials);
    const splitTag = await getTagByName(ngx, 'paperless-gpt-split', credentials);
    const autoTag = await getTagByName(ngx, 'paperless-gpt-auto', credentials);
    expect(splitTag).not.toBeNull();
    expect(autoTag).not.toBeNull();
    await addTagToDocument(ngx, original.id, splitTag!, credentials);
    await expect.poll(async () => {
      const data = await (await request.get(`${gpt}/api/split-suggestions`)).json();
      return data.results[0]?.status;
    }, { timeout: 90_000, intervals: [1000] }).toBe('review');

    // Hold metadata generation long enough to observe each child's inherited trigger tags.
    const mockUrl = `http://localhost:${environment.llmMock!.getMappedPort(8080)}`;
    const delay = await request.post(`${mockUrl}/__admin/mappings`, { data: {
      priority: 0,
      request: { method: 'POST', urlPath: '/v1/chat/completions', bodyPatterns: [{ contains: 'find a suitable document title' }] },
      response: { status: 200, fixedDelayMilliseconds: 60000, headers: { 'Content-Type': 'application/json' },
        jsonBody: { id: 'delayed-title', object: 'chat.completion', choices: [{ index: 0, message: { role: 'assistant', content: 'Child title' }, finish_reason: 'stop' }] } },
    } });
    expect(delay.ok()).toBeTruthy();
    const delayedTitleMappingID = (await delay.json()).id;

    await page.goto(gpt);
    await page.getByRole('link', { name: 'Split Suggestions', exact: true }).click();
    await page.getByRole('button', { name: /Review Apply batch/ }).click();
    await expect(page.getByRole('heading', { name: 'Review: Apply batch' })).toBeVisible();
    const firstImage = page.getByRole('img', { name: 'Page 1', exact: true });
    await expect(firstImage).toBeVisible();
    await expect.poll(() => firstImage.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
    await page.getByRole('button', { name: 'Split after page 2', exact: true }).click();
    await page.getByRole('button', { name: 'Split after page 3', exact: true }).click();
    await expect(page.getByText('3 documents: 1-3, 4, 5-6', { exact: true })).toBeVisible();
    await page.screenshot({ path: info.outputPath('split-review.png'), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('split-review-mobile.png'), fullPage: true });
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.getByRole('button', { name: 'Apply', exact: true }).click();
    await expect(page.getByRole('dialog')).toContainText('1-3, 4, 5-6');
    await expect(page.getByRole('dialog')).toContainText('kept');
    await page.getByRole('dialog').getByRole('button', { name: 'Apply split' }).click();
    await expect(page.getByRole('status')).toContainText('Split submitted');
    await expect.poll(async () => {
      const docs = await (await request.get(`${ngx}/api/documents/`, { headers: auth })).json();
      return docs.results.filter((doc: { id: number }) => doc.id !== original.id).length;
    }, { timeout: 90_000, intervals: [500] }).toBe(3);
    const docs = await (await request.get(`${ngx}/api/documents/`, { headers: auth })).json();
    const children = docs.results.filter((doc: { id: number }) => doc.id !== original.id);
    expect(children.map((doc: { page_count: number }) => doc.page_count).sort()).toEqual([1, 2, 3]);
    for (const child of children) {
      expect(child.tags).toContain(autoTag);
      expect(child.tags).not.toContain(splitTag);
    }
    const retained = docs.results.find((doc: { id: number }) => doc.id === original.id);
    expect(retained.tags).not.toContain(splitTag);
    expect(retained.tags).not.toContain(autoTag);
    await page.reload();
    await page.getByRole('button', { name: /Review Apply batch/ }).click();
    await expect(page.getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
    await expect(page.getByText('3 documents: 1-3, 4, 5-6', { exact: true })).toBeVisible();
    expect((await request.delete(`${mockUrl}/__admin/mappings/${delayedTitleMappingID}`)).ok()).toBeTruthy();
  });
  test('a kept document stays whole after refreshing rejected model boundaries', async ({ page }) => {
    const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
    await page.route('**/api/split-suggestions?*', route => route.fulfill({ json: {
      count: 1, config: { dry_run: false, delete_originals: false, tag: 'paperless-gpt-split' },
      results: [{ id: 900, document_id: 900, document_title: 'Kept document', page_count: 3, status: 'kept',
        boundaries: [{ after_page: 1, confidence: 0.95, reason: 'Rejected boundary' }], updated_at: 'kept' }],
    } }));
    await page.route('**/api/documents/900/pages/*/image?*', route => route.fulfill({ status: 503 }));
    await page.goto(`${gpt}/splitting`);
    await page.getByRole('button', { name: 'Review Kept document' }).click();
    await expect(page.getByText('1 document: 1-3', { exact: true })).toBeVisible();
    await page.reload();
    await page.getByRole('button', { name: 'Review Kept document' }).click();
    await expect(page.getByText('1 document: 1-3', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Keep as one document', exact: true })).toBeDisabled();
  });
  test('a single document can leave split review without creating children', async ({ page, request }, info) => {
    const ngx = `http://localhost:${environment.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
    const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
    await mkdir(info.outputDir, { recursive: true });
    const fixture = info.outputPath('single.pdf');
    await writeFile(fixture, batchPDF('single', 1));
    const original = await uploadDocument(ngx, fixture, 'Single document', credentials);
    const splitTag = await getTagByName(ngx, 'paperless-gpt-split', credentials);
    expect(splitTag).not.toBeNull();
    await addTagToDocument(ngx, original.id, splitTag!, credentials);
    await expect.poll(async () => {
      const data = await (await request.get(`${gpt}/api/split-suggestions`)).json();
      return data.results.find((s: { document_id: number }) => s.document_id === original.id)?.status;
    }, { timeout: 90_000, intervals: [1000] }).toBe('review');
    const before = await (await request.get(`${ngx}/api/documents/`, { headers: auth })).json();
    await page.goto(`${gpt}/splitting`);
    await page.getByRole('button', { name: 'Review Single document' }).click();
    await page.getByRole('button', { name: 'Keep as one document', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Keep document', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('released for processing');
    const data = await (await request.get(`${gpt}/api/split-suggestions`)).json();
    expect(data.results.find((s: { document_id: number }) => s.document_id === original.id).status).toBe('kept');
    const after = await (await request.get(`${ngx}/api/documents/`, { headers: auth })).json();
    expect(after.count).toBe(before.count);
    const retained = after.results.find((d: { id: number }) => d.id === original.id);
    expect(retained.page_count).toBe(1);
    expect(retained.tags).not.toContain(splitTag);
    await expect.poll(async () => {
      const document = await (await request.get(`${ngx}/api/documents/${original.id}/`, { headers: auth })).json();
      return document.title;
    }, { timeout: 90_000, intervals: [1000] }).toBe('Mock Suggested Title');
  });
});

test.describe('automatic splitting', () => {
  test.beforeAll(async () => { environment = await setupTestEnvironment({ splitDryRun: false, splitAuto: true }); });
  test.afterAll(async () => { await environment?.cleanup(); });

  test('only a complete eligible Suggestion is applied automatically', async ({ request }, info) => {
    const ngx = `http://localhost:${environment.paperlessNgx.getMappedPort(PORTS.paperlessNgx)}`;
    const gpt = `http://localhost:${environment.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`;
    const mock = `http://localhost:${environment.llmMock!.getMappedPort(8080)}`;
    const splitTag = await getTagByName(ngx, 'paperless-gpt-split', credentials);
    expect(splitTag).not.toBeNull();
    await mkdir(info.outputDir, { recursive: true });
    let expectedDocuments = 0;
    for (const kind of ['eligible', 'low-confidence', 'pagination', 'empty']) {
      const boundaries = kind === 'empty' ? [] : [
        { after_page: kind === 'pagination' ? 1 : 2, confidence: 0.99, reason: 'New sender' },
        { after_page: 4, confidence: kind === 'low-confidence' ? 0.89 : 0.9, reason: 'New sender' },
      ];
      const mapping = await request.post(`${mock}/__admin/mappings`, { data: {
        priority: 0,
        request: { method: 'POST', urlPath: '/v1/chat/completions', bodyPatterns: [{ contains: 'Split a batch scan into separate documents' }] },
        response: { status: 200, headers: { 'Content-Type': 'application/json' }, jsonBody: {
          id: 'automatic-split', object: 'chat.completion', choices: [{ index: 0, message: { role: 'assistant', content: JSON.stringify(boundaries) }, finish_reason: 'stop' }],
        } },
      } });
      expect(mapping.ok()).toBeTruthy();
      const mappingID = (await mapping.json()).id;
      const fixture = info.outputPath(`${kind}.pdf`);
      await writeFile(fixture, batchPDF(kind));
      const original = await uploadDocument(ngx, fixture, `Automatic ${kind}`, credentials);
      await addTagToDocument(ngx, original.id, splitTag!, credentials);
      const expectedStatus = kind === 'eligible' ? 'submitted' : 'review';
      await expect.poll(async () => {
        const data = await (await request.get(`${gpt}/api/split-suggestions`)).json();
        return data.results.find((s: { document_id: number }) => s.document_id === original.id)?.status;
      }, { timeout: 90_000, intervals: [1000] }).toBe(expectedStatus);
      expectedDocuments += kind === 'eligible' ? 4 : 1;
      await expect.poll(async () => (await (await request.get(`${ngx}/api/documents/`, { headers: auth })).json()).count,
        { timeout: 90_000, intervals: [1000] }).toBe(expectedDocuments);
      const source = await (await request.get(`${ngx}/api/documents/${original.id}/`, { headers: auth })).json();
      expect(source.tags.includes(splitTag)).toBe(kind !== 'eligible');
      expect((await request.delete(`${mock}/__admin/mappings/${mappingID}`)).ok()).toBeTruthy();
    }
  });
});

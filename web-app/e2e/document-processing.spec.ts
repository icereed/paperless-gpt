import { expect, Page, test } from '@playwright/test';
import path, { dirname } from 'path';
import { fileURLToPath } from 'url';
import { addTagToDocument, PORTS, setupTestEnvironment, TestEnvironment, uploadDocument } from './test-environment';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
let testEnv: TestEnvironment;
let page: Page;

test.beforeAll(async () => {
  testEnv = await setupTestEnvironment();
});

test.afterAll(async () => {
  await testEnv?.cleanup();
});

test.beforeEach(async ({ page: testPage }) => {
  page = testPage;
  await page.goto(`http://localhost:${testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`);
  await page.screenshot({ path: 'test-results/initial-state.png' });
});

test('should process document and show changes in history', async () => {
  const paperlessNgxPort = testEnv.paperlessNgx.getMappedPort(PORTS.paperlessNgx);
  const paperlessGptPort = testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt);
  const credentials = { username: 'admin', password: 'admin' };

  // 1. Upload document and add initial tag via API
  const baseUrl = `http://localhost:${paperlessNgxPort}`;
  const documentPath = path.join(__dirname, 'fixtures', 'test-document.txt');
  
  // Get the paperless-gpt tag ID
  const response = await fetch(`${baseUrl}/api/tags/?name=paperless-gpt`, {
    headers: {
      'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
    },
  });

  if (!response.ok) {
    throw new Error('Failed to fetch paperless-gpt tag');
  }

  const tags = await response.json();
  if (!tags.results || tags.results.length === 0) {
    throw new Error('paperless-gpt tag not found');
  }

  const tagId = tags.results[0].id;

  // Upload document and get ID
  const { id: documentId } = await uploadDocument(
    baseUrl,
    documentPath,
    'Original Title',
    credentials
  );

  console.log(`Document ID: ${documentId}`);

  // Add tag to document
  await addTagToDocument(
    baseUrl,
    documentId,
    tagId,
    credentials
  );

  // 2. Navigate to Paperless-GPT UI and process the document
  await page.goto(`http://localhost:${paperlessGptPort}`);
  
  // Wait for document to appear in the list
  await page.waitForSelector('.document-card', { timeout: 1000 * 60 });
  await page.screenshot({ path: 'test-results/document-loaded.png' });
  
  // Click the process button (starts an async suggestion job with progress UI)
  await page.click('button:has-text("Generate suggestions")');

  // Wait for the job to finish and the review to appear
  await page.waitForSelector('.suggestions-review', { timeout: 60000 });
  await page.screenshot({ path: 'test-results/suggestions-loaded.png' });

  // The focus view shows the document itself, readable, next to the suggestions.
  // The first load fails as if paperless-ngx were briefly unreachable, which
  // must not keep the document hidden once it is reachable again.
  const previewRoute = '**/api/documents/*/preview';
  await page.route(previewRoute, (route) =>
    route.fulfill({ status: 502, contentType: 'application/json', body: '{"error":"Failed to fetch document preview"}' })
  );
  await page.getByRole('button', { name: 'Open “Original Title” in focus view' }).click();
  const focus = page.getByRole('dialog');
  await expect(focus.getByRole('tab', { name: 'Document' })).toHaveAttribute('aria-selected', 'true');
  await expect(focus.getByText('Failed to fetch document preview')).toBeVisible();
  await page.unroute(previewRoute);
  await focus.getByRole('button', { name: 'Try again' }).click();
  const documentFrame = focus.locator('iframe');
  await expect(documentFrame).toHaveAttribute('src', `./api/documents/${documentId}/preview`);
  await expect(focus.frameLocator('iframe').locator('body')).toContainText('Invoice #12345');
  await expect(focus.getByRole('link', { name: 'Open in new tab' })).toHaveAttribute('href', `./api/documents/${documentId}/preview`);
  await expect(focus.getByRole('link', { name: 'Open in paperless-ngx' })).toHaveAttribute(
    'href',
    new RegExp(`/documents/${documentId}/details$`)
  );
  await page.screenshot({ path: 'test-results/focus-view-document.png' });

  // Arrow keys on a focused tab switch tabs rather than documents.
  await focus.getByRole('tab', { name: 'Document' }).focus();
  await page.keyboard.press('ArrowRight');
  const textTab = focus.getByRole('tab', { name: 'Extracted text' });
  await expect(textTab).toHaveAttribute('aria-selected', 'true');
  await expect(textTab).toBeFocused();
  await expect(documentFrame).toBeHidden();
  await page.keyboard.press('Tab');
  await expect(focus.getByRole('tabpanel', { name: 'Extracted text' })).toBeFocused();
  await expect(focus.getByRole('tabpanel', { name: 'Extracted text' })).toContainText('Invoice #12345');
  await focus.getByRole('button', { name: 'Close focus view' }).click();
  await expect(focus).toBeHidden();

  // Apply all remaining suggestions via the summary dialog
  await page.click('button:has-text("Apply remaining")');
  await page.getByRole('button', { name: /^Apply to \d+ documents?$/ }).click();

  // Wait for the success toast (review closes automatically once all documents are decided)
  await page.waitForSelector('div[role="status"]:has-text("Applied")', { timeout: 15000 });
  await page.screenshot({ path: 'test-results/suggestions-applied.png' });

  // 3. Check history page for the modifications
  await page.click('a:has-text("History")');
  
  // Wait for history page to load
  await page.waitForSelector('.modification-history', { timeout: 5000 });
  await page.screenshot({ path: 'test-results/history-page.png' });

  // Verify at least one modification entry exists
  const modifications = await page.locator('.undo-card').count();
  expect(modifications).toBeGreaterThan(0);

  // Verify modification details
  const firstModification = await page.locator('.undo-card:has-text("Original Title")').first();
  
  // Check if title was modified
  const titleChange = await firstModification.isVisible();
  expect(titleChange).toBeTruthy();

  // Test pagination if there are multiple modifications
  const paginationVisible = await page.locator('.pagination-controls').isVisible();
  if (paginationVisible) {
    // Click next page if available
    const nextButton = page.locator('button:has-text("Next")');
    if (await nextButton.isEnabled()) {
      await nextButton.click();
      // Wait for new items to load
      await page.waitForSelector('.undo-card');
    }
  }

  // 4. Test undo functionality
  const undoButton = await firstModification.locator('button:has-text("Undo")');
  if (await undoButton.isEnabled()) {
    await undoButton.click();
    // Wait for undo to complete. Text should change to "Undone"
    await page.waitForSelector('text=Undone');
  }
});

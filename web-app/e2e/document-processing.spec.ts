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
  await testEnv.cleanup();
});

test.beforeEach(async ({ page: testPage }) => {
  page = testPage;
  await page.goto(`http://localhost:${testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`);
  await page.screenshot({ path: 'test-results/initial-state.png' });
});

test.afterEach(async () => {
  await page.close();
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
  
  // Click the process button
  await page.click('button:has-text("Generate Suggestions")');
  
  // Wait for processing to complete
  await page.waitForSelector('.suggestions-review', { timeout: 30000 });
  await page.screenshot({ path: 'test-results/suggestions-loaded.png' });

  // Apply the suggestions
  await page.click('button:has-text("Apply")');
  
  // Wait for success message
  await page.waitForSelector('.success-modal', { timeout: 10000 });
  await page.screenshot({ path: 'test-results/suggestions-applied.png' });

  // Click "OK" on success modal
  await page.click('button:has-text("OK")');

  // 3. Check history page for the modifications
  await page.click('a:has-text("History")');
  
  // Wait for history page to load
  await page.waitForSelector('.modification-history', { timeout: 5000 });
  await page.screenshot({ path: 'test-results/history-page.png' });

  // Verify at least one modification entry exists
  const modifications = await page.locator('.undo-card').count();
  expect(modifications).toBeGreaterThan(0);

  // Verify modification details
  const firstModification = await page.locator('.undo-card').first();
  
  // Check if title was modified
  const titleChange = await firstModification.locator('text=Original Title').isVisible();
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

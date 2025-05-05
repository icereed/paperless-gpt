import { expect, Page, test } from '@playwright/test';
import path, { dirname } from 'path';
import { fileURLToPath } from 'url';
import { addTagToDocument, createTag, getTagByName, PORTS, setupTestEnvironment, TestEnvironment, uploadDocument } from './test-environment';

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
  await page.screenshot({ path: 'test-results/ocr-initial-state.png' });
});

test.afterEach(async () => {
  await page.close();
});

test('should OCR document via paperless-gpt-ocr-auto tag', async () => {
  const paperlessNgxPort = testEnv.paperlessNgx.getMappedPort(PORTS.paperlessNgx);
  const paperlessGptPort = testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt);
  const credentials = { username: 'admin', password: 'admin' };
  const baseUrl = `http://localhost:${paperlessNgxPort}`;

  // 1. Create the OCR auto tag
  console.log('Creating OCR auto tag...');
  const ocrTagId = await createTag(baseUrl, 'paperless-gpt-ocr-auto', credentials);

  // 1.1 Create the OCR complete tag
  console.log('Creating OCR complete tag...');
  const ocrCompleteTagId = await createTag(baseUrl, 'paperless-gpt-ocr-complete', credentials);
  expect(ocrTagId).not.toBeNull();
  expect(ocrCompleteTagId).not.toBeNull();

  // 2. Upload document and get ID
  const documentPath = path.join(__dirname, '..', '..', 'demo', 'ocr-example1.jpg');
  const { id: documentId, content: initialContent } = await uploadDocument(
    baseUrl,
    documentPath,
    'OCR Test Document',
    credentials
  );

  console.log(`Document ID: ${documentId}, Initial content length: ${initialContent?.length || 0}`);

  // 3. Add OCR tag to document
  await addTagToDocument(
    baseUrl,
    documentId,
    ocrTagId,
    credentials
  );

  // 4. Wait for OCR processing to complete and verify content changes
  let attempts = 0;
  const maxAttempts = 30;  // 30 seconds total wait time
  let documentContent = initialContent;
  let contentChanged = false;

  while (attempts < maxAttempts && !contentChanged) {
    // Wait 1 second between checks
    await new Promise(resolve => setTimeout(resolve, 1000));

    // Fetch latest document content
    const response = await fetch(`${baseUrl}/api/documents/${documentId}/`, {
      headers: {
        'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
      },
    });

    if (response.ok) {
      const document = await response.json();
      if (document.content !== documentContent) {
        contentChanged = true;
        console.log('Document content updated after OCR');
      }
      documentContent = document.content;
    }

    attempts++;
  }

  // 5. Verify content was updated and OCR complete tag was added
  // Check OCR complete tag was added
  const completeTagId = await getTagByName(baseUrl, 'paperless-gpt-ocr-complete', credentials);
  expect(completeTagId).not.toBeNull();

  // Check content was updated
  expect(documentContent).not.toBe(initialContent);
  expect(documentContent?.length).toBeGreaterThan(0);



  // Wait for the tag to be added to the document
  attempts = 0;
  let hasCompleteTag = false;
  while (attempts < maxAttempts && !hasCompleteTag) {
    await new Promise(resolve => setTimeout(resolve, 1000));

    const docResponse = await fetch(`${baseUrl}/api/documents/${documentId}/`, {
      headers: {
        'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
      },
    });

    if (docResponse.ok) {
      const doc = await docResponse.json();
      if (doc.tags.includes(completeTagId)) {
        hasCompleteTag = true;
        console.log('OCR complete tag added to document');
      }
    }

    attempts++;
  }

  expect(hasCompleteTag).toBeTruthy();

  // 6. Check UI for processing status
  await page.goto(`http://localhost:${paperlessGptPort}`);

  // Take screenshot of final state
  await page.screenshot({ path: 'test-results/ocr-final-state.png' });
});

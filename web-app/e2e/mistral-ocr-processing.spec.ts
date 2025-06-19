import { expect, Page, test } from '@playwright/test';
import path, { dirname } from 'path';
import { fileURLToPath } from 'url';
import { addTagToDocument, createTag, getTagByName, PORTS, setupTestEnvironment, TestEnvironment, uploadDocument } from './test-environment';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
let testEnv: TestEnvironment;
let page: Page;

/**
 * Displays a detailed comparison between original and enhanced OCR content
 * @param originalContent - The original document content before OCR processing
 * @param enhancedContent - The enhanced document content after OCR processing
 * @param options - Configuration options for the diff display
 */
function displayOCRComparison(
  originalContent: string | null | undefined,
  enhancedContent: string | null | undefined,
  options: {
    previewLength?: number;
    title?: string;
    showMetrics?: boolean;
  } = {}
) {
  const {
    previewLength = 200,
    title = 'OCR Content Comparison',
    showMetrics = true
  } = options;

  console.log(`\n📊 ${title}:`);
  console.log('='.repeat(60));
  
  // Original content section
  console.log(`📄 Original content (${originalContent?.length || 0} chars):`);
  console.log('─'.repeat(30));
  if (originalContent && originalContent.length > 0) {
    const originalPreview = originalContent.length > previewLength 
      ? originalContent.substring(0, previewLength) + '...' 
      : originalContent;
    console.log(originalPreview);
  } else {
    console.log('(Empty or no content)');
  }
  
  // Enhanced content section
  console.log(`\n✨ Enhanced OCR content (${enhancedContent?.length || 0} chars):`);
  console.log('─'.repeat(30));
  if (enhancedContent && enhancedContent.length > 0) {
    const enhancedPreview = enhancedContent.length > previewLength 
      ? enhancedContent.substring(0, previewLength) + '...' 
      : enhancedContent;
    console.log(enhancedPreview);
  } else {
    console.log('(Empty or no content)');
  }

  // Metrics section
  if (showMetrics) {
    console.log('\n📈 Improvement metrics:');
    console.log('─'.repeat(30));
    
    const originalLength = originalContent?.length || 0;
    const enhancedLength = enhancedContent?.length || 0;
    const lengthDiff = enhancedLength - originalLength;
    const percentageChange = originalLength > 0 
      ? ((lengthDiff / originalLength) * 100).toFixed(1) 
      : 'N/A';
    
    console.log(`Length change: ${lengthDiff > 0 ? '+' : ''}${lengthDiff} characters (${percentageChange}%)`);
    
    // Word count comparison
    const originalWords = originalContent ? originalContent.split(/\s+/).filter(w => w.length > 0).length : 0;
    const enhancedWords = enhancedContent ? enhancedContent.split(/\s+/).filter(w => w.length > 0).length : 0;
    const wordDiff = enhancedWords - originalWords;
    console.log(`Word count change: ${wordDiff > 0 ? '+' : ''}${wordDiff} words (${originalWords} → ${enhancedWords})`);
    
    // Content quality indicators
    if (originalLength > 0 && enhancedLength > 0) {
      const avgWordLengthOriginal = originalLength / Math.max(originalWords, 1);
      const avgWordLengthEnhanced = enhancedLength / Math.max(enhancedWords, 1);
      console.log(`Average word length: ${avgWordLengthOriginal.toFixed(1)} → ${avgWordLengthEnhanced.toFixed(1)} chars`);
    }
  }
  
  console.log('='.repeat(60));
}

test.beforeAll(async () => {
  testEnv = await setupTestEnvironment({
    // Override default configuration for Mistral OCR
    ocrProvider: 'mistral_ocr',
    processMode: 'whole_pdf'
  });
});

test.afterAll(async () => {
  await testEnv.cleanup();
});

test.beforeEach(async ({ page: testPage }) => {
  page = testPage;
  await page.goto(`http://localhost:${testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt)}`);
  await page.screenshot({ path: 'test-results/mistral-ocr-initial-state.png' });
});

test.afterEach(async () => {
  await page.close();
});

test('should process multi-page PDF with Mistral OCR using whole_pdf mode', async () => {
  // Skip test if MISTRAL_API_KEY is not provided
  if (!process.env.MISTRAL_API_KEY) {
    console.log('Skipping Mistral OCR test - MISTRAL_API_KEY not provided');
    return;
  }

  const paperlessNgxPort = testEnv.paperlessNgx.getMappedPort(PORTS.paperlessNgx);
  const paperlessGptPort = testEnv.paperlessGpt.getMappedPort(PORTS.paperlessGpt);
  const credentials = { username: 'admin', password: 'admin' };
  const baseUrl = `http://localhost:${paperlessNgxPort}`;

  console.log('Testing Mistral OCR with multi-page PDF in whole_pdf mode...');

  // 1. Create the OCR auto tag
  console.log('Creating OCR auto tag...');
  const ocrTagId = await createTag(baseUrl, 'paperless-gpt-ocr-auto', credentials);

  // 1.1 Create the OCR complete tag
  console.log('Creating OCR complete tag...');
  const ocrCompleteTagId = await createTag(baseUrl, 'paperless-gpt-ocr-complete', credentials);
  expect(ocrTagId).not.toBeNull();
  expect(ocrCompleteTagId).not.toBeNull();

  // 2. Upload the five-page PDF document
  const documentPath = path.join(__dirname, '..', '..', 'tests', 'pdf', 'five-pager.pdf');
  console.log(`Uploading document from: ${documentPath}`);
  
  const { id: documentId, content: initialContent } = await uploadDocument(
    baseUrl,
    documentPath,
    'Mistral OCR Test - Five Page PDF',
    credentials
  );

  console.log(`Document ID: ${documentId}, Initial content length: ${initialContent?.length || 0}`);

  // 3. Add OCR tag to document to trigger processing
  console.log('Adding OCR auto tag to trigger Mistral OCR processing...');
  await addTagToDocument(
    baseUrl,
    documentId,
    ocrTagId,
    credentials
  );

  // 4. Wait for OCR processing to complete and verify content changes
  console.log('Waiting for Mistral OCR processing to complete...');
  let attempts = 0;
  const maxAttempts = 60;  // 60 seconds total wait time (Mistral may take longer than simple LLM OCR)
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
        console.log('Document content updated after Mistral OCR processing');
        console.log(`New content length: ${document.content?.length || 0}`);
      }
      documentContent = document.content;
    }

    attempts++;
    
    // Log progress every 10 seconds
    if (attempts % 10 === 0) {
      console.log(`Still waiting for OCR processing... (${attempts}/${maxAttempts} seconds)`);
    }
  }

  // 5. Verify content was updated and OCR complete tag was added
  console.log('Verifying OCR processing results...');

  // Check OCR complete tag was added
  const completeTagId = await getTagByName(baseUrl, 'paperless-gpt-ocr-complete', credentials);
  expect(completeTagId).not.toBeNull();

  // Check content was updated (should be significantly different and longer)
  expect(contentChanged).toBeTruthy();
  expect(documentContent).not.toBe(initialContent);
  expect(documentContent?.length).toBeGreaterThan(0);
  
  // For a 5-page PDF, we should get substantial text content
  expect(documentContent?.length).toBeGreaterThan(100);

  // Should contain "Musée royal d'Histoire naturelle de. Belgique"
  expect(documentContent).toContain("Musée royal d'Histoire naturelle de. Belgique");
  
  console.log(`OCR processing successful! Extracted ${documentContent?.length} characters of text.`);

  // Show a diff between original and enhanced content
  displayOCRComparison(initialContent, documentContent, {
    title: 'Mistral OCR Enhancement Results',
    previewLength: 300,
    showMetrics: true
  });

  // 6. Wait for the OCR complete tag to be added to the document
  console.log('Waiting for OCR complete tag to be added...');
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
        console.log('OCR complete tag successfully added to document');
      }
    }

    attempts++;
  }

  expect(hasCompleteTag).toBeTruthy();

  // 7. Verify the UI shows the processing status
  await page.goto(`http://localhost:${paperlessGptPort}`);
  
  // Take final screenshot
  await page.screenshot({ path: 'test-results/mistral-ocr-final-state.png' });

  console.log('✅ Mistral OCR E2E test completed successfully!');
  console.log(`✅ Processed 5-page PDF with whole_pdf mode`);
  console.log(`✅ Extracted ${documentContent?.length} characters of text`);
  console.log(`✅ OCR complete tag added successfully`);
});

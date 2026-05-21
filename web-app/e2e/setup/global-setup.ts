import { chromium } from '@playwright/test';
import * as nodeFetch from 'node-fetch';

// Polyfill fetch for Node.js environment
if (!globalThis.fetch) {
  const globals = globalThis as typeof globalThis & {
    fetch: typeof fetch;
    Headers: typeof Headers;
    Request: typeof Request;
    Response: typeof Response;
    FormData: typeof FormData;
  };
  globals.fetch = nodeFetch.default as unknown as typeof fetch;
  globals.Headers = nodeFetch.Headers as unknown as typeof Headers;
  globals.Request = nodeFetch.Request as unknown as typeof Request;
  globals.Response = nodeFetch.Response as unknown as typeof Response;
  globals.FormData = nodeFetch.FormData as unknown as typeof FormData;
}

async function globalSetup() {
  // Install Playwright browser if needed
  const browser = await chromium.launch();
  await browser.close();

  // Load environment variables
  if (!process.env.OPENAI_API_KEY) {
    console.warn('Warning: OPENAI_API_KEY environment variable is not set');
  }
}

export default globalSetup;

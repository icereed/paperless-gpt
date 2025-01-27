import { chromium } from '@playwright/test';
import * as nodeFetch from 'node-fetch';

// Polyfill fetch for Node.js environment
if (!globalThis.fetch) {
  (globalThis as any).fetch = nodeFetch.default;
  (globalThis as any).Headers = nodeFetch.Headers;
  (globalThis as any).Request = nodeFetch.Request;
  (globalThis as any).Response = nodeFetch.Response;
  (globalThis as any).FormData = nodeFetch.FormData;
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

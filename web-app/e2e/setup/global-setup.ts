import { chromium } from '@playwright/test';
import * as nodeFetch from 'node-fetch';

// Polyfill fetch for Node.js environment
if (!globalThis.fetch) {
  const nodeGlobals = globalThis as typeof globalThis & {
    fetch: typeof nodeFetch.default;
    Headers: typeof nodeFetch.Headers;
    Request: typeof nodeFetch.Request;
    Response: typeof nodeFetch.Response;
    FormData: typeof nodeFetch.FormData;
  };
  nodeGlobals.fetch = nodeFetch.default;
  nodeGlobals.Headers = nodeFetch.Headers;
  nodeGlobals.Request = nodeFetch.Request;
  nodeGlobals.Response = nodeFetch.Response;
  nodeGlobals.FormData = nodeFetch.FormData;
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

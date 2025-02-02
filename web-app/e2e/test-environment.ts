import { Browser, chromium } from '@playwright/test';
import * as fs from 'fs';
import { GenericContainer, Network, StartedTestContainer, Wait } from 'testcontainers';

export interface TestEnvironment {
  paperlessNgx: StartedTestContainer;
  paperlessGpt: StartedTestContainer;
  browser: Browser;
  cleanup: () => Promise<void>;
}

export const PORTS = {
  paperlessNgx: 8000,
  paperlessGpt: 8080,
};

export async function setupTestEnvironment(): Promise<TestEnvironment> {
  console.log('Setting up test environment...');
  const paperlessPort = PORTS.paperlessNgx;
  const gptPort = PORTS.paperlessGpt;

  // Create a network for the containers
  const network = await new Network().start();

  console.log('Starting Redis container...');
  const redis = await new GenericContainer('redis:7')
    .withNetwork(network)
    .withNetworkAliases('redis')
    .start();

  console.log('Starting Postgres container...');
  const postgres = await new GenericContainer('postgres:15')
    .withNetwork(network)
    .withNetworkAliases('postgres')
    .withEnvironment({
      POSTGRES_DB: 'paperless',
      POSTGRES_USER: 'paperless',
      POSTGRES_PASSWORD: 'paperless'
    })
    .start();

  console.log('Starting Paperless-ngx container...');
  const paperlessNgx = await new GenericContainer('ghcr.io/paperless-ngx/paperless-ngx:latest')
    .withNetwork(network)
    .withNetworkAliases('paperless-ngx')
    .withEnvironment({
      PAPERLESS_URL: `http://localhost:${paperlessPort}`,
      PAPERLESS_SECRET_KEY: 'change-me',
      PAPERLESS_ADMIN_USER: 'admin',
      PAPERLESS_ADMIN_PASSWORD: 'admin',
      PAPERLESS_TIME_ZONE: 'Europe/Berlin',
      PAPERLESS_OCR_LANGUAGE: 'eng',
      PAPERLESS_REDIS: 'redis://redis:6379',
      PAPERLESS_DBHOST: 'postgres',
      PAPERLESS_DBNAME: 'paperless',
      PAPERLESS_DBUSER: 'paperless',
      PAPERLESS_DBPASS: 'paperless'
    })
    .withExposedPorts(paperlessPort)
    .withWaitStrategy(Wait.forHttp('/api/', paperlessPort))
    .start();

  const mappedPort = paperlessNgx.getMappedPort(paperlessPort);
  console.log(`Paperless-ngx container started, mapped port: ${mappedPort}`);
  // Create required tag before starting paperless-gpt
  const baseUrl = `http://localhost:${mappedPort}`;
  const credentials = { username: 'admin', password: 'admin' };

  try {
    console.log('Creating paperless-gpt tag...');
    await createTag(baseUrl, 'paperless-gpt', credentials);
  } catch (error) {
    console.error('Failed to create paperless-gpt tag:', error);
    await paperlessNgx.stop();
    throw error;
  }

  console.log('Starting Paperless-gpt container...');
  const paperlessGptImage = process.env.PAPERLESS_GPT_IMAGE || 'icereed/paperless-gpt:e2e';
  console.log(`Using image: ${paperlessGptImage}`);
  const paperlessGpt = await new GenericContainer(paperlessGptImage)
    .withNetwork(network)
    .withEnvironment({
      PAPERLESS_BASE_URL: `http://paperless-ngx:${paperlessPort}`,
      PAPERLESS_API_TOKEN: await getApiToken(baseUrl, credentials),
      LLM_PROVIDER: "openai",
      LLM_MODEL: "gpt-4o-mini",
      LLM_LANGUAGE: "english",
      OPENAI_API_KEY: process.env.OPENAI_API_KEY || '',
    })
    .withExposedPorts(gptPort)
    .withWaitStrategy(Wait.forHttp('/', gptPort))
    .start();
  console.log('Paperless-gpt container started');

  console.log('Launching browser...');
  const browser = await chromium.launch();
  console.log('Browser launched');

  const cleanup = async () => {
    console.log('Cleaning up test environment...');
    await browser.close();
    await paperlessGpt.stop();
    await paperlessNgx.stop();
    await redis.stop();
    await postgres.stop();
    await network.stop();
    console.log('Test environment cleanup completed');
  };

  console.log('Test environment setup completed');
  return {
    paperlessNgx,
    paperlessGpt,
    browser,
    cleanup,
  };
}

export async function waitForElement(page: any, selector: string, timeout = 5000): Promise<void> {
  await page.waitForSelector(selector, { timeout });
}

export interface PaperlessDocument {
  id: number;
  title: string;
  content: string;
  tags: number[];
}

// Helper to upload a document via Paperless-ngx API
export async function uploadDocument(
  baseUrl: string,
  filePath: string,
  title: string,
  credentials: { username: string; password: string }
): Promise<PaperlessDocument> {
  console.log(`Uploading document: ${title} from ${filePath}`);
  const formData = new FormData();
  const fileData = await fs.promises.readFile(filePath);
  formData.append('document', new Blob([fileData]));
  formData.append('title', title);

  // Initial upload to get task ID
  const uploadResponse = await fetch(`${baseUrl}/api/documents/post_document/`, {
    method: 'POST',
    body: formData,
    headers: {
      'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
    },
  });

  if (!uploadResponse.ok) {
    console.error(`Upload failed with status ${uploadResponse.status}: ${uploadResponse.statusText}`);
    throw new Error(`Failed to upload document: ${uploadResponse.statusText}`);
  }
  
  const task_id = await uploadResponse.json();
  
  // Poll the tasks endpoint until document is processed
  while (true) {
    console.log(`Checking task status for ID: ${task_id}`);
    const taskResponse = await fetch(`${baseUrl}/api/tasks/?task_id=${task_id}`, {
      headers: {
        'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
      },
    });

    if (!taskResponse.ok) {
      throw new Error(`Failed to check task status: ${taskResponse.statusText}`);
    }

    const taskResultArr = await taskResponse.json();
    console.log(`Task status: ${JSON.stringify(taskResultArr)}`);

    if (taskResultArr.length === 0) {
      continue;
    }
    const taskResult = taskResultArr[0];
    // Check if task is completed
    if (taskResult.status === 'SUCCESS' && taskResult.id) {
      console.log(`Document processed successfully with ID: ${taskResult.id}`);
      
      // Fetch the complete document details
      const documentResponse = await fetch(`${baseUrl}/api/documents/${taskResult.id}/`, {
        headers: {
          'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
        },
      });

      if (!documentResponse.ok) {
        throw new Error(`Failed to fetch document details: ${documentResponse.statusText}`);
      }

      return await documentResponse.json();
    }
    
    // Check for failure
    if (taskResult.status === 'FAILED') {
      throw new Error(`Document processing failed: ${taskResult.result}`);
    }
    
    // Wait before polling again
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
}
// Helper to create a tag via Paperless-ngx API
export async function createTag(
  baseUrl: string,
  name: string,
  credentials: { username: string; password: string }
): Promise<number> {
  console.log(`Creating tag: ${name}`);
  const response = await fetch(`${baseUrl}/api/tags/`, {
    method: 'POST',
    body: JSON.stringify({ name }),
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
    },
  });

  if (!response.ok) {
    console.error(`Tag creation failed with status ${response.status}: ${response.statusText}`);
    throw new Error(`Failed to create tag: ${response.statusText}`);
  }

  const tag = await response.json();
  console.log(`Tag created successfully with ID: ${tag.id}`);
  return tag.id;
}

// Helper to get an API token
export async function getApiToken(
  baseUrl: string,
  credentials: { username: string; password: string }
): Promise<string> {
  console.log('Fetching API token');
  const response = await fetch(`${baseUrl}/api/token/`, {
    method: 'POST',
    body: new URLSearchParams({
      username: credentials.username,
      password: credentials.password,
    }),
  });

  if (!response.ok) {
    console.error(`API token fetch failed with status ${response.status}: ${response.statusText}`);
    throw new Error(`Failed to fetch API token: ${response.statusText}`);
  }

  const token = await response.json();
  console.log(`API token fetched successfully: ${token.token}`);
  return token.token;
}

// Helper to add a tag to a document
export async function addTagToDocument(
  baseUrl: string,
  documentId: number,
  tagId: number,
  credentials: { username: string; password: string }
): Promise<void> {
  console.log(`Adding tag ${tagId} to document ${documentId}`);
  const response = await fetch(`${baseUrl}/api/documents/${documentId}/`, {
    method: 'PATCH',
    body: JSON.stringify({
      tags: [tagId],
    }),
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Basic ' + btoa(`${credentials.username}:${credentials.password}`),
    },
  });

  if (!response.ok) {
    console.error(`Tag addition failed with status ${response.status}: ${response.statusText}`);
    throw new Error(`Failed to add tag to document: ${response.statusText}`);
  }
  console.log('Tag added successfully');
}

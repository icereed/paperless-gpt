import * as path from 'path';
import { fileURLToPath } from 'url';
import { expect, test } from '@playwright/test';

import { uploadDocument } from './test-environment';

const fixturePath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'fixtures', 'test-document.txt');
const baseUrl = 'http://paperless.test';
const credentials = { username: 'admin', password: 'admin' };

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

test.describe('uploadDocument task responses', () => {
  test('throws after a bounded number of terminal SUCCESS responses without an ID', async () => {
    let taskPolls = 0;
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (input) => {
      const url = String(input);
      if (url.endsWith('/api/documents/post_document/')) {
        return jsonResponse('task-id');
      }
      if (url.includes('/api/tasks/')) {
        taskPolls++;
        return jsonResponse([{ status: 'SUCCESS', result_data: {}, related_document_ids: [] }]);
      }
      throw new Error(`Unexpected request: ${url}`);
    };

    try {
      await expect(uploadDocument(baseUrl, fixturePath, 'missing ID', credentials)).rejects.toThrow(
        /SUCCESS without a document ID after 5 polls: \[\{"status":"SUCCESS","result_data":\{\},"related_document_ids":\[\]\}\]/
      );
      expect(taskPolls).toBe(5);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  for (const testCase of [
    { name: 'result_data document ID', task: { status: 'SUCCESS', result_data: { document_id: 41 } }, documentID: 41 },
    { name: 'related document ID', task: { status: 'SUCCESS', related_document_ids: [42] }, documentID: 42 },
  ]) {
    test(`returns a document for ${testCase.name}`, async () => {
      const originalFetch = globalThis.fetch;
      globalThis.fetch = async (input) => {
        const url = String(input);
        if (url.endsWith('/api/documents/post_document/')) {
          return jsonResponse('task-id');
        }
        if (url.includes('/api/tasks/')) {
          return jsonResponse([testCase.task]);
        }
        if (url.endsWith(`/api/documents/${testCase.documentID}/`)) {
          return jsonResponse({ id: testCase.documentID, title: 'uploaded', content: '', tags: [] });
        }
        throw new Error(`Unexpected request: ${url}`);
      };

      try {
        await expect(uploadDocument(baseUrl, fixturePath, 'valid ID', credentials)).resolves.toMatchObject({
          id: testCase.documentID,
        });
      } finally {
        globalThis.fetch = originalFetch;
      }
    });
  }
});

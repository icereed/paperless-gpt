# E2E Test Pipeline Fixes - Analysis and Resolution

## Executive Summary

This document details the comprehensive analysis and fixes applied to the e2e test pipeline for the paperless-gpt project. The tests were experiencing failures in CI due to multiple architectural and configuration issues that have now been resolved.

## Issues Identified and Fixed

### 1. Worker Parallelism Conflict ⚠️ CRITICAL

#### Problem Analysis
- **Location**: `web-app/package.json` line 12
- **Issue**: The npm script `test:e2e` was configured with `--workers 4`
- **Root Cause**: This command-line argument overrides the `workers: 1` setting in `playwright.config.ts`

#### Impact
When tests ran with 4 workers:
- Up to 4 test files could execute simultaneously
- Each test file spawns its own complete TestContainers environment:
  - 1x Redis container
  - 1x PostgreSQL container  
  - 1x Paperless-ngx container
  - 1x Paperless-gpt container
- **Total**: Up to 16 containers running simultaneously in CI

This caused:
- Port mapping conflicts between parallel container stacks
- Resource exhaustion (CPU, memory, disk I/O)
- Unreliable test execution with intermittent failures
- Container startup timeouts

#### Solution
```diff
- "test:e2e": "playwright test --workers 4",
+ "test:e2e": "playwright test",
```

**Why this works**:
- Removes the CLI override, allowing `playwright.config.ts` settings to take effect
- Tests now run sequentially with `workers: 1` from config
- Each test environment is fully set up and torn down before the next starts
- Eliminates container port conflicts and resource contention

---

### 2. Missing ANTHROPIC_API_KEY Environment Variable ⚠️ CRITICAL

#### Problem Analysis
- **Location**: `.github/workflows/docker-build-and-push.yml` e2e-tests job
- **Issue**: `ANTHROPIC_API_KEY` was not passed to the test environment
- **Test Files Affected**:
  - `web-app/e2e/anthropic-ocr-processing.spec.ts` (2 tests)

#### Impact
- Anthropic OCR tests would skip in CI (when API key is provided but not passed)
- Test coverage gap for Anthropic OCR functionality
- No validation of Anthropic integration in automated testing

#### Solution
```diff
  env:
    CI: true
    DEBUG: testcontainers:containers
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    MISTRAL_API_KEY: ${{ secrets.MISTRAL_API_KEY }}
+   ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
    PAPERLESS_GPT_IMAGE: ${{ env.PAPERLESS_GPT_IMAGE }}
```

**Why this works**:
- Tests can now access Anthropic API when secret is configured in repository
- Provides complete test coverage across all supported OCR providers
- Tests gracefully skip when API key is not available (development/fork scenarios)

---

### 3. Insufficient Test Timeout ⚠️ HIGH

#### Problem Analysis
- **Location**: `web-app/playwright.config.ts` line 9
- **Issue**: Test timeout was 120,000ms (2 minutes)
- **Test Requirements**: Some OCR processing operations require significantly more time

#### Timing Analysis
For `anthropic-ocr-processing.spec.ts` multi-page PDF test:
```
Container startup:           ~30-60 seconds
OCR content processing:      ~120 seconds (maxAttempts = 120)
Tag completion check:        ~120 seconds (maxAttempts = 120)
Additional operations:       ~30 seconds
---
Total worst-case time:       ~300+ seconds
```

With a 120-second timeout, tests would fail before completing legitimate operations.

#### Solution
```diff
- timeout: 120000, // Increased timeout for container startup
+ timeout: 300000, // 5 minutes - increased for OCR processing with multiple providers
```

**Why this works**:
- Provides adequate time for full OCR processing cycles
- Accounts for API latency variations across providers
- Prevents false negatives from legitimate slow operations
- Still catches actual hangs within reasonable timeframe

---

### 4. Improper Page Lifecycle Management ⚠️ MEDIUM

#### Problem Analysis
- **Location**: All test spec files in `afterEach` hooks
- **Issue**: Tests were manually calling `page.close()` on Playwright fixture pages
- **Pattern**:
```typescript
test.beforeEach(async ({ page: testPage }) => {
  page = testPage;
  // ... use page
});

test.afterEach(async () => {
  await page.close();  // ❌ PROBLEMATIC
});
```

#### Impact
- Playwright automatically manages fixture page lifecycle
- Manual close calls could interfere with Playwright's cleanup
- Potential for "page already closed" errors
- Unnecessary complexity in test code

#### Solution
Removed `afterEach` hooks entirely from all test files:
- `web-app/e2e/document-processing.spec.ts`
- `web-app/e2e/ocr-document-processing.spec.ts`
- `web-app/e2e/mistral-ocr-processing.spec.ts`
- `web-app/e2e/anthropic-ocr-processing.spec.ts`

**Why this works**:
- Playwright's fixture system handles page creation and cleanup automatically
- Each test gets a fresh page instance from the fixture
- Cleanup happens reliably after test completion
- Follows Playwright best practices and patterns

---

### 5. Unused Browser Creation ⚠️ LOW

#### Problem Analysis
- **Location**: `web-app/e2e/test-environment.ts`
- **Issue**: `setupTestEnvironment()` was creating a browser instance that was never used
- **Code Pattern**:
```typescript
// Creating browser in test environment setup
const browser = await chromium.launch();
console.log('Browser launched');

return {
  paperlessNgx,
  paperlessGpt,
  browser,  // ❌ Never used
  cleanup,
};
```

#### Impact Analysis
- `testEnv.browser` was stored but never accessed by any test
- Tests used Playwright's built-in browser fixture instead
- Wasted resources: extra browser process + memory
- Added unnecessary complexity to test setup
- Potential confusion for future maintainers

#### Solution
1. Removed `browser: Browser` from `TestEnvironment` interface
2. Removed browser creation and launch code
3. Removed browser cleanup from cleanup function
4. Removed unused `waitForElement` helper function (also used Browser/Page types)

**Changes**:
```diff
- import { Browser, chromium, Page } from '@playwright/test';
  import * as fs from 'fs';
  import { GenericContainer, Network, StartedTestContainer, Wait } from 'testcontainers';

  export interface TestEnvironment {
    paperlessNgx: StartedTestContainer;
    paperlessGpt: StartedTestContainer;
-   browser: Browser;
    cleanup: () => Promise<void>;
  }

  // ... in setupTestEnvironment():
- console.log('Launching browser...');
- const browser = await chromium.launch();
- console.log('Browser launched');

  const cleanup = async () => {
    console.log('Cleaning up test environment...');
-   await browser.close();
    await paperlessGpt.stop();
    // ...
  };

  return {
    paperlessNgx,
    paperlessGpt,
-   browser,
    cleanup,
  };

- export async function waitForElement(page: Page, selector: string, timeout = 5000): Promise<void> {
-   await page.waitForSelector(selector, { timeout });
- }
```

**Why this works**:
- Reduces resource usage during test execution
- Simplifies test environment setup
- Removes potential source of confusion
- Follows principle of not creating unused resources

---

## Test Architecture Overview

### Container Stack Per Test File
Each test file creates via `setupTestEnvironment()`:
```
Network (isolated)
├── Redis container
├── PostgreSQL container  
├── Paperless-ngx container (with admin user)
└── Paperless-gpt container (application under test)
```

### Sequential Execution Flow
```
Test File 1 (document-processing.spec.ts)
├── beforeAll: Setup containers
├── Test 1: Process document and check history
└── afterAll: Cleanup containers

↓ (containers fully cleaned up)

Test File 2 (ocr-document-processing.spec.ts)
├── beforeAll: Setup containers  
├── Test 1: OCR with auto tag
└── afterAll: Cleanup containers

↓ (containers fully cleaned up)

... and so on
```

### Why Sequential Execution is Critical
1. **Port Mapping**: Each container needs dynamic ports
2. **Resources**: Each stack needs ~4GB RAM + CPU
3. **TestContainers**: Designed for isolated environments
4. **Reliability**: Prevents race conditions and conflicts

---

## Verification Checklist

### Before These Fixes
- ❌ Tests failed due to container port conflicts
- ❌ Resource exhaustion in CI
- ❌ Anthropic tests not executing
- ❌ Timeout failures on long OCR operations
- ⚠️ Page lifecycle management issues
- ⚠️ Unnecessary browser resource usage

### After These Fixes
- ✅ Sequential test execution (workers: 1)
- ✅ No container port conflicts
- ✅ All API keys properly passed to tests
- ✅ Adequate timeout for OCR operations
- ✅ Proper Playwright fixture usage
- ✅ Optimized resource usage

---

## Testing Recommendations

### For CI/CD
1. Monitor test duration - should be longer (sequential) but more reliable
2. Verify no container startup timeouts
3. Check that all OCR provider tests execute when keys are provided
4. Validate test artifacts (screenshots, videos) are captured on failures

### For Local Development
1. Build test image first: `docker build . -t icereed/paperless-gpt:e2e`
2. Set environment variables for desired OCR providers
3. Run tests: `npm run test:e2e` (from web-app directory)
4. Use `npm run test:e2e:ui` for interactive debugging

### Expected Timings
- Container startup per test file: ~30-60s
- Simple document processing test: ~1-2 min
- OCR tests (image): ~2-3 min
- OCR tests (multi-page PDF): ~3-5 min
- Total suite (4 test files): ~10-15 min

---

## Files Modified

### Configuration Files
1. `web-app/package.json` - Removed `--workers 4` override
2. `web-app/playwright.config.ts` - Increased timeout to 300s
3. `.github/workflows/docker-build-and-push.yml` - Added ANTHROPIC_API_KEY

### Test Files
4. `web-app/e2e/document-processing.spec.ts` - Removed page.close()
5. `web-app/e2e/ocr-document-processing.spec.ts` - Removed page.close()
6. `web-app/e2e/mistral-ocr-processing.spec.ts` - Removed page.close()
7. `web-app/e2e/anthropic-ocr-processing.spec.ts` - Removed page.close()

### Test Infrastructure
8. `web-app/e2e/test-environment.ts` - Removed unused browser and helper

---

## Related Documentation

- Test setup instructions: `web-app/e2e/README.md`
- Playwright configuration: `web-app/playwright.config.ts`
- CI workflow: `.github/workflows/docker-build-and-push.yml`
- TestContainers docs: https://testcontainers.com/

---

## Conclusion

These fixes address all identified issues in the e2e test pipeline. The tests should now run reliably in CI with:
- Proper sequential execution preventing container conflicts
- Complete API key coverage for all OCR providers
- Adequate timeouts for long-running operations
- Correct Playwright fixture usage
- Optimized resource consumption

The changes are minimal, focused, and follow testing best practices.

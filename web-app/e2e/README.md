# End-to-End (E2E) Testing Documentation

This directory contains end-to-end tests for the paperless-gpt web application using Playwright and TestContainers.

## Overview

The E2E tests validate the complete integration between the paperless-gpt frontend, backend API, and paperless-ngx system by:

- Running real Docker containers for paperless-ngx, Redis, and PostgreSQL
- Testing actual document processing workflows
- Verifying OCR enhancement functionality with multiple providers
- Testing the complete user interface interactions

## Prerequisites

### Required Software
- Docker and Docker Compose
- Node.js 18+ with npm
- Playwright browsers (automatically installed)

### Build the Test Image

**CRITICAL**: Before running E2E tests, you must build the paperless-gpt Docker image that the tests will use:

```bash
# From the project root directory
docker build . -t icereed/paperless-gpt:e2e
```

This builds the complete application (frontend + backend) into a Docker image. The E2E tests will use this image to create the paperless-gpt container.

**Note**: You must rebuild this image whenever you make changes to the application code that you want to test.

### Required Environment Variables

For **OpenAI LLM OCR tests**:
```bash
export OPENAI_API_KEY="your_openai_api_key_here"
```

For **Mistral OCR tests**:
```bash
export MISTRAL_API_KEY="your_mistral_api_key_here"
```

### Optional Environment Variables
```bash
# Use specific paperless-gpt Docker image (defaults to icereed/paperless-gpt:e2e)
export PAPERLESS_GPT_IMAGE="your_custom_image:tag"

# Base URL for paperless-gpt (defaults to http://localhost:8080)
export PAPERLESS_GPT_URL="http://localhost:8080"
```

**Important**: The `PAPERLESS_GPT_IMAGE` environment variable allows you to specify which Docker image the tests should use. If not set, it defaults to `icereed/paperless-gpt:e2e`, which you must build with the `docker build` command shown above.

## Test Architecture

### Container Stack
Each test spins up a complete environment using TestContainers:

1. **PostgreSQL** - Database for paperless-ngx
2. **Redis** - Cache and task queue for paperless-ngx
3. **Paperless-ngx** - Document management system
4. **Paperless-gpt** - AI enhancement service (your application)

### Network Configuration
- All containers run on an isolated Docker network
- Ports are dynamically mapped to avoid conflicts
- API authentication uses admin:admin credentials

### Test Data
- Test documents in `fixtures/` directory
- PDF samples in `../../tests/pdf/` directory
- Predefined tags: `paperless-gpt`, `paperless-gpt-ocr-auto`, `paperless-gpt-ocr-complete`

## Running Tests

### Quick Start

1. **Build the Docker image** (required before first run or after code changes):
   ```bash
   # From project root
   docker build . -t icereed/paperless-gpt:e2e
   ```

2. **Set environment variables**:
   ```bash
   export OPENAI_API_KEY="your_openai_api_key_here"
   # Optional: export MISTRAL_API_KEY="your_mistral_api_key_here"
   ```

3. **Run the tests**:
   ```bash
   # From web-app directory
   npm run test:e2e
   ```

### Run All Tests
```bash
npm run test:e2e
```

### Run Tests with UI Mode (Interactive)
```bash
npm run test:e2e:ui
```

### Run Specific Test File
```bash
npx playwright test document-processing.spec.ts
```

### Run with Debug Mode
```bash
npx playwright test --debug
```

### Run in Headed Mode (See Browser)
```bash
npx playwright test --headed
```

## Test Files

### `document-processing.spec.ts`
**Purpose**: Tests the complete document processing workflow with LLM-based OCR.

**What it tests**:
- Document upload to paperless-ngx
- Adding tags to trigger processing
- AI-powered title and metadata generation
- UI interaction for applying suggestions
- History tracking of modifications
- Undo functionality

**Duration**: ~2-3 minutes

### `mistral-ocr-processing.spec.ts`
**Purpose**: Tests OCR enhancement using Mistral's dedicated OCR service.

**What it tests**:
- Multi-page PDF processing
- Mistral OCR API integration
- Content extraction and enhancement
- OCR completion tagging
- Performance metrics comparison

**Duration**: ~3-5 minutes (Mistral processing can be slower)

**Requirements**: `MISTRAL_API_KEY` environment variable

### `ocr-document-processing.spec.ts`
**Purpose**: Tests OCR functionality with various document types and providers.

**What it tests**:
- Different OCR providers (LLM vs. dedicated OCR)
- Image-based OCR processing modes
- Content quality improvement metrics
- Error handling for OCR failures

**Duration**: ~2-4 minutes

## Test Configuration

### Playwright Configuration (`../playwright.config.ts`)
```typescript
{
  testDir: './e2e',
  timeout: 120000,        // 2 minutes per test
  workers: 1,             // Sequential execution for container stability
  fullyParallel: false,   // Prevents container conflicts
  retries: 2,             // Retry failed tests in CI
  reporter: 'html',       // Generate HTML report
}
```

### Global Setup (`setup/global-setup.ts`)
- Installs Playwright browsers
- Validates environment variables
- Sets up fetch polyfill for Node.js

## Test Utilities (`test-environment.ts`)

### Key Functions

#### `setupTestEnvironment(config?)`
Creates and starts the complete container stack.

**Parameters**:
- `config.ocrProvider`: `'llm'` (default) or `'mistral_ocr'`
- `config.processMode`: `'image'` (default) or `'whole_pdf'`

**Returns**: TestEnvironment with container references and cleanup function

#### `uploadDocument(baseUrl, filePath, title, credentials)`
Uploads a document to paperless-ngx and waits for processing completion.

#### `createTag(baseUrl, name, credentials)`
Creates a tag in paperless-ngx (or returns existing tag ID).

#### `addTagToDocument(baseUrl, documentId, tagId, credentials)`
Associates a tag with a document to trigger processing.

### OCR Configuration Examples

```typescript
// LLM-based OCR (OpenAI)
const testEnv = await setupTestEnvironment({
  ocrProvider: 'llm',
  processMode: 'image'
});

// Mistral dedicated OCR
const testEnv = await setupTestEnvironment({
  ocrProvider: 'mistral_ocr',
  processMode: 'whole_pdf'
});
```

## Debugging Tests

### Screenshots and Videos
Tests automatically capture:
- Screenshots on failure: `test-results/`
- Videos on failure: `test-results/`
- Traces for debugging: `test-results/`

### Common Debug Commands
```bash
# View test report after run
npx playwright show-report

# Run single test with full output
npx playwright test document-processing.spec.ts --headed --timeout=300000

# Debug specific test interactively
npx playwright test --debug document-processing.spec.ts
```

### Container Logs
Access container logs during test execution:
```bash
# In another terminal during test run
docker ps
docker logs <container_id>
```

## Troubleshooting

### Common Issues

#### "TestContainers timeout" Error
- Ensure Docker is running
- Check available disk space (containers need ~2GB)
- Verify network connectivity for image downloads

#### "Image not found" or "Failed to pull image" Error
**This usually means you forgot to build the test image.**

```bash
# Solution: Build the required Docker image
docker build . -t icereed/paperless-gpt:e2e
```

The E2E tests expect this specific image tag. If you're using a different image, set the `PAPERLESS_GPT_IMAGE` environment variable:
```bash
export PAPERLESS_GPT_IMAGE="your_custom_image:tag"
```

#### "Browser not installed" Error
```bash
npx playwright install chromium
```

#### "TestContainers timeout" Error
- Ensure Docker is running
- Check available disk space (containers need ~2GB)
- Verify network connectivity for image downloads

#### "API Key missing" Warnings
- Set `OPENAI_API_KEY` for LLM tests
- Set `MISTRAL_API_KEY` for Mistral tests
- Tests will skip gracefully if keys are missing

#### Container Port Conflicts
Tests use dynamic port mapping to avoid conflicts. If issues persist:
```bash
docker ps
docker stop $(docker ps -q)  # Stop all containers
```

#### Slow Test Performance
- Increase timeouts in `playwright.config.ts`
- Use `workers: 1` to prevent resource contention
- Check Docker resource allocation (RAM/CPU)

### Environment-Specific Issues

#### CI/CD Environments
- Tests require Docker-in-Docker capability
- Network restrictions may affect container downloads
- Consider using pre-built images for faster startup

#### Apple Silicon (M1/M2) Macs
- Use ARM64-compatible images when available
- Some containers may need platform specification:
  ```bash
  docker pull --platform linux/amd64 postgres:15
  ```

## Test Data Management

### Test Documents
- `fixtures/test-document.txt`: Simple text invoice for basic processing
- `../../tests/pdf/five-pager.pdf`: Multi-page PDF for OCR testing
- `../../tests/pdf/sample.pdf`: Single-page PDF sample

### Creating New Test Documents
1. Add files to `fixtures/` directory
2. Use realistic document formats (PDF, images, text)
3. Include variety of content types (invoices, reports, forms)
4. Consider OCR complexity (handwriting, tables, multiple languages)

## Performance Expectations

### Typical Test Timings
- Container startup: 30-60 seconds
- Document upload: 5-10 seconds
- AI processing: 15-30 seconds
- OCR enhancement: 30-120 seconds
- Total test duration: 2-5 minutes per test

### Resource Usage
- RAM: ~4GB during peak execution
- Disk: ~2GB for container images
- Network: ~500MB for initial image downloads

## Contributing

### Adding New Tests
1. Create new `.spec.ts` file in this directory
2. Follow existing patterns for container setup
3. Use descriptive test names and comments
4. Include proper cleanup in `afterAll` hooks
5. Add screenshots for debugging

### Test Best Practices
- Use `test.beforeAll()` for expensive setup
- Implement proper cleanup to avoid resource leaks
- Use meaningful assertions with clear error messages
- Add console logging for debugging complex workflows
- Verify both UI state and API responses

### Code Style
- Follow existing TypeScript patterns
- Use async/await for all async operations
- Include proper error handling
- Document complex test scenarios

## Continuous Integration

### GitHub Actions Integration
The tests run automatically in CI with:
- Docker-in-Docker capability
- Environment variable injection
- Artifact collection for failures
- Multi-architecture support

### Local CI Simulation
```bash
# Simulate CI environment locally
npm run test:e2e -- --reporter=github
```

## Resources

- [Playwright Documentation](https://playwright.dev/docs/intro)
- [TestContainers Documentation](https://testcontainers.com/)
- [Paperless-ngx API Documentation](https://docs.paperless-ngx.com/api/)
- [Docker Compose Reference](https://docs.docker.com/compose/)
# paperless-gpt Development Instructions

paperless-gpt is a Go backend with React/TypeScript frontend that provides AI-powered document processing for paperless-ngx. The application uses LLMs for OCR enhancement, document title/tag generation, and metadata extraction.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

**CRITICAL BUILD AND TEST REQUIREMENTS:**
- **NEVER CANCEL BUILDS OR LONG-RUNNING COMMANDS** - Set timeouts of 120+ seconds for builds, 600+ seconds for Go builds with dependencies
- All builds and tests MUST complete fully before proceeding to next steps
- Docker builds require network access and will fail in restricted environments

### Bootstrap, Build, and Test the Repository:

**System Dependencies:**
```bash
# Install required system packages
sudo apt-get update
sudo apt-get install -y mupdf libmupdf-dev
```

**Frontend Build (web-app/):**
```bash
cd web-app
npm install                    # Takes ~24 seconds - NEVER CANCEL, timeout: 300s
npm run build                  # Takes ~7 seconds - NEVER CANCEL, timeout: 120s
cp -r dist ../                 # Copy build output for Go embedding
```

**Backend Build:**
```bash
go mod download                # Takes ~19 seconds - NEVER CANCEL, timeout: 300s
go build -o paperless-gpt      # Takes ~65 seconds - NEVER CANCEL, timeout: 600s
```

**Complete Build Process:**
```bash
# Full build pipeline (frontend + backend)
cd web-app && npm install && npm run build && cp -r dist ../ && cd ..
go mod download
go build -o paperless-gpt
# Total time: ~110 seconds - NEVER CANCEL, timeout: 900s
```

### Testing:

**Go Tests:**
```bash
go test ./...                  # Takes ~47 seconds - NEVER CANCEL, timeout: 600s
# NOTE: Some tests may fail due to network restrictions (tiktoken.GetEncoding)
# This is expected in restricted environments and not a code issue
```

**Frontend Tests:**
```bash
cd web-app
npm test                       # Currently placeholder (echo "TODO") - Takes <1 second
```

**Linting:**
```bash
# Frontend linting
cd web-app && npm run lint     # Takes ~2 seconds - May show TypeScript errors
# Go formatting check
gofmt -l .                     # Instant - Shows files needing formatting
```

### Running the Application:

**Minimal Configuration:**
```bash
# Create basic environment (modify values as needed)
export PAPERLESS_BASE_URL="http://localhost:8000"
export PAPERLESS_API_TOKEN="your_token_here"
export LLM_PROVIDER="ollama"
export LLM_MODEL="test_model"
export OLLAMA_HOST="http://localhost:11434"
```

**Start Application:**
```bash
./paperless-gpt               # Starts web server on port 8080
# Application gracefully handles missing paperless-ngx connection
# Web UI accessible at http://localhost:8080
```

## Validation

**MANUAL VALIDATION REQUIREMENT:**
After building and running the application, you MUST test actual functionality by:

1. **Start the application** with minimal configuration
2. **Verify web server starts** on port 8080
3. **Check API endpoints** are registered (visible in startup logs)
4. **Confirm prompt templates** are created from defaults
5. **Validate frontend assets** are served from dist/ directory

**Docker Validation:**
```bash
# Docker build (requires network access)
docker build -t paperless-gpt .  # Takes 5+ minutes - NEVER CANCEL, timeout: 1800s
# NOTE: May fail in restricted environments due to Alpine package access
```

**E2E Tests:**
```bash
cd web-app
npm run test:e2e              # Requires Docker and TestContainers
# NOTE: Won't work in restricted environments without Docker access
```

## Common Tasks

**Build and Exercise Changes:**
1. Always build frontend first: `cd web-app && npm install && npm run build && cp -r dist ../`
2. Then build backend: `go mod download && go build -o paperless-gpt`
3. Run tests: `go test ./...` (expect network-related failures in restricted environments)
4. Start application and verify web server starts correctly

**Project Structure Reference:**
```bash
# Repository root structure
paperless-gpt/
├── web-app/                   # React/TypeScript frontend
│   ├── src/                   # React source code
│   ├── dist/                  # Built frontend (created by npm run build)
│   ├── e2e/                   # Playwright E2E tests
│   └── package.json           # Frontend dependencies
├── ocr/                       # OCR provider implementations
├── default_prompts/           # Default AI prompt templates
├── docs/                      # Documentation
├── .github/workflows/         # CI/CD pipeline
├── main.go                    # Go backend entry point
├── Dockerfile                 # Multi-stage Docker build
├── go.mod                     # Go dependencies
└── README.md                  # Comprehensive project documentation
```

**Key Files to Check After Changes:**
- Always verify `web-app/dist/` contains built frontend after changes to React code
- Check `prompts/` directory is created and populated after first run
- Monitor `config/settings.json` for application configuration
- Review Go tests in `*_test.go` files for validation patterns

**Critical Timing Information:**
- **Frontend build**: 7 seconds (npm run build)
- **Go module download**: 19 seconds  
- **Go build**: 65 seconds
- **Go tests**: 47 seconds (with network warnings)
- **Docker build**: 5+ minutes (when network allows)
- **Total development cycle**: ~2 minutes for full build + test

**Always Use These Timeouts:**
- npm commands: 300+ seconds
- go build: 600+ seconds  
- go test: 600+ seconds
- docker build: 1800+ seconds

## Debugging Common Issues

- **mupdf build errors**: Install `mupdf libmupdf-dev` system packages
- **Frontend not loading**: Ensure `cp -r dist ../` after npm build
- **Test failures**: Network-related tiktoken failures are expected in restricted environments
- **Docker build fails**: Requires external network access to Alpine repositories
- **Application won't start**: Check required environment variables (PAPERLESS_BASE_URL, etc.)

## CI/CD Pipeline

The GitHub Actions workflow (`.github/workflows/docker-build-and-push.yml`) includes:
- Go and Node.js setup
- Frontend and backend testing
- Multi-architecture Docker builds (AMD64 + ARM64)
- Playwright E2E tests with TestContainers
- Image publishing to Docker Hub and GHCR

Always run `npm run lint` and verify Go formatting with `gofmt -l .` before committing changes to ensure CI pipeline success.
# Technical Context

## Technology Stack

### Backend (Go)
- **Runtime**: Go
- **Key Libraries**:
  - langchaingo: LLM integration
  - logrus: Structured logging
  - net/http: API server

### Frontend (React/TypeScript)
- **Framework**: React with TypeScript
- **Build Tool**: Vite
- **Testing**: Playwright
- **Styling**: Tailwind CSS
- **Package Manager**: npm

### Infrastructure
- **Containerization**: Docker
- **Deployment**: Docker Compose
- **CI/CD**: GitHub Actions

## Development Setup

### Prerequisites
1. Docker and Docker Compose
2. Go development environment
3. Node.js and npm
4. Access to LLM provider (OpenAI or Ollama)

### Local Development Steps
1. Clone repository
2. Configure environment variables
3. Start paperless-ngx instance
4. Build and run paperless-gpt
5. Access web interface

### Testing Steps (Required Before Commits)
1. **Unit Tests**:
   ```bash
   go test .
   ```

2. **E2E Tests**:
   ```bash
   docker build . -t icereed/paperless-gpt:e2e
   cd web-app && npm run test:e2e
   ```

These tests MUST be run and pass before considering any task complete.

## Configuration

### Environment Variables

#### Required Variables
```
PAPERLESS_BASE_URL=http://paperless-ngx:8000
PAPERLESS_API_TOKEN=your_paperless_api_token
LLM_PROVIDER=openai|ollama
LLM_MODEL=model_name
```

#### Optional Variables
```
PAPERLESS_PUBLIC_URL=public_url
MANUAL_TAG=paperless-gpt
AUTO_TAG=paperless-gpt-auto
OPENAI_API_KEY=key (if using OpenAI)
OPENAI_BASE_URL=custom_url
LLM_LANGUAGE=English
OLLAMA_HOST=host_url
VISION_LLM_PROVIDER=provider
VISION_LLM_MODEL=model
AUTO_OCR_TAG=tag
OCR_LIMIT_PAGES=5
LOG_LEVEL=info
```

### Docker Configuration
- Network configuration for service communication
- Volume mounts for prompts and persistence
- Resource limits and scaling options
- Port mappings for web interface

### LLM Provider Setup

#### OpenAI Configuration
- API key management
- Model selection
- Base URL configuration (for custom endpoints)
- Vision API access for OCR

#### Ollama Configuration
- Server setup and hosting
- Model installation and management
- Network access configuration
- Resource allocation

### Custom Prompts

#### Template Files
- title_prompt.tmpl
- tag_prompt.tmpl
- ocr_prompt.tmpl
- correspondent_prompt.tmpl

#### Template Variables
- Language
- Content
- AvailableTags
- OriginalTags
- Title
- AvailableCorrespondents
- BlackList

## Technical Constraints

### Performance Considerations
- Token limits for LLM requests
- OCR page limits
- Concurrent processing limits
- Network bandwidth requirements

### Security Requirements
- API token security
- Environment variable management
- Network isolation
- Data privacy considerations

### Integration Requirements
- paperless-ngx compatibility
- LLM provider API compatibility
- Docker environment compatibility
- Web browser compatibility

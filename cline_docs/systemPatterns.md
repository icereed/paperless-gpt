# System Patterns

## Architecture Overview

### 1. Microservices Architecture
- **paperless-gpt**: AI processing service (Go)
- **paperless-ngx**: Document management system (external)
- Communication via REST API
- Docker-based deployment

### 2. Backend Architecture (Go)

#### Core Components
- **API Server**: HTTP handlers for document processing
- **LLM Integration**: Abstraction for multiple AI providers
- **Template Engine**: Dynamic prompt generation
- **Document Processor**: Handles OCR and metadata generation

#### Key Patterns
- **Template-Based Prompts**: Customizable templates for different AI tasks
- **Content Truncation**: Smart content limiting based on token counts
- **Concurrent Processing**: Goroutines for parallel document processing
- **Mutex-Protected Resources**: Thread-safe template access
- **Error Propagation**: Structured error handling across layers

### 3. Frontend Architecture (React/TypeScript)

#### Components
- Document Processor
- Suggestion Review
- Document Cards
- Sidebar Navigation
- Success Modal

#### State Management
- Local component state
- Props for component communication
- API integration for data fetching

### 4. Integration Patterns

#### API Communication
- RESTful endpoints
- JSON payload structure
- Token-based authentication
- Error response handling

#### LLM Provider Integration
- Provider abstraction layer
- Support for multiple providers (OpenAI, Ollama)
- Configurable models and parameters
- Vision model support for OCR

### 5. Data Flow

#### Document Processing Flow (Manual)
1. Document tagged in paperless-ngx
2. paperless-gpt detects tagged documents
3. AI processing (title/tags/correspondent generation)
4. Manual review or auto-apply
5. Update back to paperless-ngx

#### Document Processing Flow (Auto)
1. Document tagged in paperless-ngx with some 'auto' tag (env: AUTO_TAG)
2. paperless-gpt automatically processes documents
3. AI processing (title/tags/correspondent generation)
4. Auto-apply results back to paperless-ngx

#### OCR Processing Flow
1. Image/PDF input
2. Vision model processing
3. Text extraction and cleanup
4. Integration with document processing

### 6. Security Patterns
- API token authentication
- Environment-based configuration
- Docker container isolation
- Rate limiting and token management

### 7. Development Patterns
- Clear separation of concerns
- Dependency injection
- Interface-based design
- Concurrent processing with safety
- Comprehensive error handling
- Template-based customization

### 8. Testing Patterns
- Unit tests for core logic
- Integration tests for API
- E2E tests for web interface
- Test fixtures and mocks
- Playwright for frontend testing

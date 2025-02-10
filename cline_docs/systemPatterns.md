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

## OCR System Patterns

### OCR Provider Architecture

#### 1. Provider Interface
- Common interface for all OCR implementations
- Methods for image processing
- Configuration through standardized Config struct
- Resource management patterns

#### 2. LLM Provider Implementation
- Supports OpenAI and Ollama vision models
- Base64 encoding for OpenAI requests
- Binary format for Ollama requests
- Template-based OCR prompts

#### 3. Google Document AI Provider
- Enterprise-grade OCR processing
- MIME type validation
- Processor configuration via environment
- Regional endpoint support

### Logging Patterns

#### 1. Provider Initialization
```
[INFO] Initializing OCR provider: llm
[INFO] Using LLM OCR provider (provider=ollama, model=minicpm-v)
```

#### 2. Processing Logs
```
[DEBUG] Starting OCR processing
[DEBUG] Image dimensions (width=800, height=1200)
[DEBUG] Using binary image format for non-OpenAI provider
[DEBUG] Sending request to vision model
[INFO] Successfully processed image (content_length=1536)
```

#### 3. Error Logging
```
[ERROR] Failed to decode image: invalid format
[ERROR] Unsupported file type: image/webp
[ERROR] Failed to get response from vision model
```

### Error Handling Patterns

#### 1. Configuration Validation
- Required parameter checks
- Environment variable validation
- Provider-specific configuration
- Connection testing

#### 2. Processing Errors
- Image format validation
- MIME type checking
- Content processing errors
- Provider-specific error handling

#### 3. Error Propagation
- Detailed error contexts
- Original error wrapping
- Logging with error context
- Recovery mechanisms

### Processing Flow

#### 1. Document Processing
```
Document Tagged → OCR Provider Selected → Image Processing → Text Extraction → Content Update
```

#### 2. Provider Selection
```
Config Check → Provider Initialization → Resource Setup → Provider Ready
```

#### 3. Error Recovery
```
Error Detection → Logging → Cleanup → Error Propagation
```

These patterns ensure consistent behavior across OCR providers while maintaining proper logging and error handling throughout the system.

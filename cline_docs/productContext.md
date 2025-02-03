# Product Context

## Project Purpose
paperless-gpt is an AI-powered companion application designed to enhance the document management capabilities of paperless-ngx by automating document organization tasks through advanced AI technologies.

## Problems Solved
1. Manual Document Organization
   - Eliminates time-consuming manual tagging and title creation
   - Reduces human error in document categorization
   - Streamlines document processing workflow

2. OCR Quality
   - Improves text extraction from poor quality scans
   - Provides context-aware OCR capabilities
   - Handles complex document layouts better than traditional OCR

3. Document Categorization
   - Automates correspondent identification
   - Provides intelligent tag suggestions
   - Generates meaningful document titles

## Core Functionality

### 1. LLM-Enhanced OCR
- Uses Large Language Models for better text extraction
- Handles messy or low-quality scans effectively
- Provides context-aware text interpretation

### 2. Automatic Document Processing
- Title Generation: Creates descriptive titles based on content
- Tag Generation: Suggests relevant tags from existing tag set
- Correspondent Identification: Automatically detects document senders/recipients

### 3. Integration Features
- Seamless paperless-ngx integration
- Docker-based deployment
- Customizable prompt templates
- Support for multiple LLM providers (OpenAI, Ollama)

### 4. User Interface
- Web-based management interface
- Manual review capabilities
- Batch processing support
- Auto-processing workflow option

## Usage Flow
1. Documents are tagged with specific markers (e.g., 'paperless-gpt')
2. System processes documents using AI/LLM capabilities
3. Results can be automatically applied or manually reviewed
4. Processed documents are updated in paperless-ngx

## Configuration Options
- Manual vs. automatic processing
- LLM provider selection
- Language preferences
- Processing limits and constraints
- Custom prompt templates

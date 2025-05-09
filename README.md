# paperless-gpt

[![License](https://img.shields.io/github/license/icereed/paperless-gpt)](LICENSE)
[![Discord Banner](https://img.shields.io/badge/Join%20us%20on-Discord-blue?logo=discord)](https://discord.gg/fJQppDH2J7)
[![Docker Pulls](https://img.shields.io/docker/pulls/icereed/paperless-gpt)](https://hub.docker.com/r/icereed/paperless-gpt)
[![GitHub Container Registry](https://img.shields.io/badge/GHCR-Package-181717?logo=github)](https://github.com/icereed/paperless-gpt/pkgs/container/paperless-gpt)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![GitHub Sponsors](https://img.shields.io/badge/Sponsor-icereed-ea4aaa?logo=github-sponsors)](https://github.com/sponsors/icereed)

![Screenshot](./paperless-gpt-screenshot.png)

**paperless-gpt** seamlessly pairs with [paperless-ngx][paperless-ngx] to generate **AI-powered document titles** and **tags**, saving you hours of manual sorting. While other tools may offer AI chat features, **paperless-gpt** stands out by **supercharging OCR with LLMs**-ensuring high accuracy, even with tricky scans. If you're craving next-level text extraction and effortless document organization, this is your solution.

https://github.com/user-attachments/assets/bd5d38b9-9309-40b9-93ca-918dfa4f3fd4

> **❤️ Support This Project**  
> If paperless-gpt is helping you organize your documents and saving you time, please consider [sponsoring its development](https://github.com/sponsors/icereed). Your support helps ensure continued improvements and maintenance!

---

## Key Highlights

1. **LLM-Enhanced OCR**  
   Harness Large Language Models (OpenAI or Ollama) for **better-than-traditional** OCR—turn messy or low-quality scans into context-aware, high-fidelity text.

2. **Use specialized AI OCR services**

   - **LLM OCR**: Use OpenAI or Ollama to extract text from images.
   - **Google Document AI**: Leverage Google's powerful Document AI for OCR tasks.
   - **Azure Document Intelligence**: Use Microsoft's enterprise OCR solution.
   - **Docling Server**: Self-hosted OCR and document conversion service

3. **Automatic Title, Tag & Created Date Generation**  
   No more guesswork. Let the AI do the naming and categorizing. You can easily review suggestions and refine them if needed.

4. **Supports DeepSeek reasoning models in Ollama**  
   Greatly enhance accuracy by using a reasoning model like `deepseek-r1:8b`. The perfect tradeoff between privacy and performance! Of course, if you got enough GPUs or NPUs, a bigger model will enhance the experience.

5. **Automatic Correspondent Generation**  
   Automatically identify and generate correspondents from your documents, making it easier to track and organize your communications.

6. **Searchable & Selectable PDFs**  
   Generate PDFs with transparent text layers positioned accurately over each word, making your documents both searchable and selectable while preserving the original appearance.

7. **Extensive Customization**

   - **Prompt Templates**: Tweak your AI prompts to reflect your domain, style, or preference.
   - **Tagging**: Decide how documents get tagged—manually, automatically, or via OCR-based flows.
   - **PDF Processing**: Configure how OCR-enhanced PDFs are handled, with options to save locally or upload to paperless-ngx.

8. **Simple Docker Deployment**  
   A few environment variables, and you're off! Compose it alongside paperless-ngx with minimal fuss.

9. **Unified Web UI**

   - **Manual Review**: Approve or tweak AI's suggestions.
   - **Auto Processing**: Focus only on edge cases while the rest is sorted for you.

---

## Table of Contents

- [paperless-gpt](#paperless-gpt)
  - [Key Highlights](#key-highlights)
  - [Table of Contents](#table-of-contents)
  - [Getting Started](#getting-started)
    - [Prerequisites](#prerequisites)
    - [Installation](#installation)
      - [Docker Compose](#docker-compose)
      - [Manual Setup](#manual-setup)
  - [OCR Providers](#ocr-providers)
    - [1. LLM-based OCR (Default)](#1-llm-based-ocr-default)
    - [2. Azure Document Intelligence](#2-azure-document-intelligence)
    - [3. Google Document AI](#3-google-document-ai)
    - [4. Docling Server](#4-docling-server)
  - [Enhanced OCR Features](#enhanced-ocr-features)
    - [PDF Text Layer Generation](#pdf-text-layer-generation)
    - [Local File Saving](#local-file-saving)
    - [PDF Upload to paperless-ngx](#pdf-upload-to-paperless-ngx)
    - [Metadata Copying Limitations](#metadata-copying-limitations)
    - [Safety Features](#safety-features)
    - [Usage Recommendations](#usage-recommendations)
  - [Configuration](#configuration)
    - [Environment Variables](#environment-variables)
    - [Custom Prompt Templates](#custom-prompt-templates)
      - [Template Variables](#template-variables)
  - [LLM-Based OCR: Compare for Yourself](#llm-based-ocr-compare-for-yourself)
    - [Example 1](#example-1)
    - [Example 2](#example-2)
    - [How It Works](#how-it-works)
  - [Usage](#usage)
  - [Troubleshooting](#troubleshooting)
    - [Working with Local LLMs](#working-with-local-llms)
      - [Token Management](#token-management)
    - [PDF Processing Issues](#pdf-processing-issues)
  - [Contributing](#contributing)
  - [Support the Project](#support-the-project)
  - [License](#license)
  - [Star History](#star-history)
  - [Disclaimer](#disclaimer)

---

## Getting Started

### Prerequisites

- [Docker][docker-install] installed.
- A running instance of [paperless-ngx][paperless-ngx].
- Access to an LLM provider:
  - **OpenAI**: An API key with models like `gpt-4o` or `gpt-3.5-turbo`.
  - **Ollama**: A running Ollama server with models like `deepseek-r1:8b`.

### Installation

#### Docker Compose

Here's an example `docker-compose.yml` to spin up **paperless-gpt** alongside paperless-ngx:

```yaml
services:
  paperless-ngx:
    image: ghcr.io/paperless-ngx/paperless-ngx:latest
    # ... (your existing paperless-ngx config)

  paperless-gpt:
    # Use one of these image sources:
    image: icereed/paperless-gpt:latest  # Docker Hub
    # image: ghcr.io/icereed/paperless-gpt:latest  # GitHub Container Registry
    environment:
      PAPERLESS_BASE_URL: "http://paperless-ngx:8000"
      PAPERLESS_API_TOKEN: "your_paperless_api_token"
      PAPERLESS_PUBLIC_URL: "http://paperless.mydomain.com" # Optional
      MANUAL_TAG: "paperless-gpt" # Optional, default: paperless-gpt
      AUTO_TAG: "paperless-gpt-auto" # Optional, default: paperless-gpt-auto
      # LLM Configuration - Choose one:
      
      # Option 1: Standard OpenAI
      LLM_PROVIDER: "openai"
      LLM_MODEL: "gpt-4o"
      OPENAI_API_KEY: "your_openai_api_key"
      
      # Option 2: Mistral
      # LLM_PROVIDER: "mistral"
      # LLM_MODEL: "mistral-large-latest"
      # MISTRAL_API_KEY: "your_mistral_api_key"

      # Option 3: Azure OpenAI
      # LLM_PROVIDER: "openai"
      # LLM_MODEL: "your-deployment-name"
      # OPENAI_API_KEY: "your_azure_api_key"
      # OPENAI_API_TYPE: "azure"
      # OPENAI_BASE_URL: "https://your-resource.openai.azure.com"
      
      # Option 3: Ollama (Local)
      # LLM_PROVIDER: "ollama"
      # LLM_MODEL: "deepseek-r1:8b"
      # OLLAMA_HOST: "http://host.docker.internal:11434"
      # TOKEN_LIMIT: 1000 # Recommended for smaller models
      
      # Optional LLM Settings
      # LLM_LANGUAGE: "English" # Optional, default: English

      # OCR Configuration - Choose one:
      # Option 1: LLM-based OCR
      OCR_PROVIDER: "llm" # Default OCR provider
      VISION_LLM_PROVIDER: "ollama" # openai or ollama
      VISION_LLM_MODEL: "minicpm-v" # minicpm-v (ollama) or gpt-4o (openai)
      OLLAMA_HOST: "http://host.docker.internal:11434" # If using Ollama

      # Option 2: Google Document AI
      # OCR_PROVIDER: 'google_docai'       # Use Google Document AI
      # GOOGLE_PROJECT_ID: 'your-project'  # Your GCP project ID
      # GOOGLE_LOCATION: 'us'              # Document AI region
      # GOOGLE_PROCESSOR_ID: 'processor-id' # Your processor ID
      # GOOGLE_APPLICATION_CREDENTIALS: '/app/credentials.json' # Path to service account key

      # Option 3: Azure Document Intelligence
      # OCR_PROVIDER: 'azure'              # Use Azure Document Intelligence
      # AZURE_DOCAI_ENDPOINT: 'your-endpoint' # Your Azure endpoint URL
      # AZURE_DOCAI_KEY: 'your-key'        # Your Azure API key
      # AZURE_DOCAI_MODEL_ID: 'prebuilt-read' # Optional, defaults to prebuilt-read
      # AZURE_DOCAI_TIMEOUT_SECONDS: '120'  # Optional, defaults to 120 seconds
      # AZURE_DOCAI_OUTPUT_CONTENT_FORMAT: 'text' # Optional, defaults to 'text', other valid option is 'markdown'
              # 'markdown' requires the 'prebuilt-layout' model

      # Enhanced OCR Features
      CREATE_LOCAL_HOCR: "false" # Optional, save hOCR files locally
      LOCAL_HOCR_PATH: "/app/hocr" # Optional, path for hOCR files
      CREATE_LOCAL_PDF: "false" # Optional, save enhanced PDFs locally
      LOCAL_PDF_PATH: "/app/pdf" # Optional, path for PDF files
      PDF_UPLOAD: "false" # Optional, upload enhanced PDFs to paperless-ngx
      PDF_REPLACE: "false" # Optional and DANGEROUS, delete original after upload
      PDF_COPY_METADATA: "true" # Optional, copy metadata from original document
      PDF_OCR_TAGGING: "true" # Optional, add tag to processed documents
      PDF_OCR_COMPLETE_TAG: "paperless-gpt-ocr-complete" # Optional, tag name

      # Option 4: Docling Server
      # OCR_PROVIDER: 'docling'              # Use a Docling server
      # DOCLING_URL: 'http://your-docling-server:port' # URL of your Docling instance

      AUTO_OCR_TAG: "paperless-gpt-ocr-auto" # Optional, default: paperless-gpt-ocr-auto
      OCR_LIMIT_PAGES: "5" # Optional, default: 5. Set to 0 for no limit.
      LOG_LEVEL: "info" # Optional: debug, warn, error
    volumes:
      - ./prompts:/app/prompts # Mount the prompts directory
      # For Google Document AI:
      - ${HOME}/.config/gcloud/application_default_credentials.json:/app/credentials.json
      # For local hOCR and PDF saving:
      - ./hocr:/app/hocr # Only if CREATE_LOCAL_HOCR is true
      - ./pdf:/app/pdf # Only if CREATE_LOCAL_PDF is true
    ports:
      - "8080:8080"
    depends_on:
      - paperless-ngx
```

**Pro Tip**: Replace placeholders with real values and read the logs if something looks off.

#### Manual Setup

1. **Clone the Repository**
   ```bash
   git clone https://github.com/icereed/paperless-gpt.git
   cd paperless-gpt
   ```
2. **Create a `prompts` Directory**
   ```bash
   mkdir prompts
   ```
3. **Build the Docker Image**
   ```bash
   docker build -t paperless-gpt .
   ```
4. **Run the Container**
   ```bash
   docker run -d \
     -e PAPERLESS_BASE_URL='http://your_paperless_ngx_url' \
     -e PAPERLESS_API_TOKEN='your_paperless_api_token' \
     -e LLM_PROVIDER='openai' \
     -e LLM_MODEL='gpt-4o' \
     -e OPENAI_API_KEY='your_openai_api_key' \
     -e LLM_LANGUAGE='English' \
     -e VISION_LLM_PROVIDER='ollama' \
     -e VISION_LLM_MODEL='minicpm-v' \
     -e LOG_LEVEL='info' \
     -v $(pwd)/prompts:/app/prompts \
     -p 8080:8080 \
     paperless-gpt
   ```

---
## OCR Providers

For detailed provider-specific documentation:
- [Mistral AI Integration](docs/mistral_llm.md)

paperless-gpt supports four different OCR providers, each with unique strengths and capabilities:

### 1. LLM-based OCR (Default)
- **Key Features**:
  - Uses vision-capable LLMs like gpt-4o or MiniCPM-V
  - High accuracy with complex layouts and difficult scans
  - Context-aware text recognition
  - Self-correcting capabilities for OCR errors
- **Best For**:
  - Complex or unusual document layouts
  - Poor quality scans
  - Documents with mixed languages
- **Configuration**:
  ```yaml
  OCR_PROVIDER: "llm"
  VISION_LLM_PROVIDER: "openai" # or "ollama"
  VISION_LLM_MODEL: "gpt-4o" # or "minicpm-v"
  ```

### 2. Azure Document Intelligence
- **Key Features**:
  - Enterprise-grade OCR solution
  - Prebuilt models for common document types
  - Layout preservation and table detection
  - Fast processing speeds
- **Best For**:
  - Business documents and forms
  - High-volume processing
  - Documents requiring layout analysis
- **Configuration**:
  ```yaml
  OCR_PROVIDER: "azure"
  AZURE_DOCAI_ENDPOINT: "https://your-endpoint.cognitiveservices.azure.com/"
  AZURE_DOCAI_KEY: "your-key"
  AZURE_DOCAI_MODEL_ID: "prebuilt-read" # optional
  AZURE_DOCAI_TIMEOUT_SECONDS: "120" # optional
  AZURE_DOCAI_OUTPUT_CONTENT_FORMAT: "text" # optional, defaults to text, other valid option is 'markdown'
    # 'markdown' requires the 'prebuilt-layout' model
  ```

### 3. Google Document AI
- **Key Features**:
  - Enterprise-grade OCR/HTR solution
  - Specialized document processors
  - Strong form field detection
  - Multi-language support
  - High accuracy on structured documents
  - **Exclusive hOCR generation** for creating searchable PDFs with text layers
  - **Only provider that supports** enhanced PDF generation features
- **Best For**:
  - Forms and structured documents
  - Documents with tables
  - Multi-language documents
  - Handwritten text (HTR)
- **Configuration**:
  ```yaml
  OCR_PROVIDER: "google_docai"
  GOOGLE_PROJECT_ID: "your-project"
  GOOGLE_LOCATION: "us"
  GOOGLE_PROCESSOR_ID: "processor-id"
  CREATE_LOCAL_HOCR: "true" # Optional, for hOCR generation
  LOCAL_HOCR_PATH: "/app/hocr" # Optional, default path
  CREATE_LOCAL_PDF: "true" # Optional, for applying OCR to PDF
  LOCAL_PDF_PATH: "/app/pdf" # Optional, default path 
  ```

### 4. Docling Server
- **Key Features**:
  - Self-hosted OCR and document conversion service
  - Supports various input and output formats (including text)
  - Utilizes multiple OCR engines (EasyOCR, Tesseract, etc.)
  - Can be run locally or in a private network
- **Best For**:
  - Users who prefer a self-hosted solution
  - Environments where data privacy is paramount
  - Processing a wide variety of document types
- **Configuration**:
  ```yaml
  OCR_PROVIDER: "docling"
  DOCLING_URL: "http://your-docling-server:port"
  ```

## Enhanced OCR Features

paperless-gpt now includes powerful OCR enhancements that go beyond basic text extraction:

> **Important Note**: The PDF text layer generation and hOCR features are currently **only supported with Google Document AI** as the OCR provider. These features are not available when using LLM-based OCR or Azure Document Intelligence.

### PDF Text Layer Generation

- **Searchable & Selectable PDFs**: Creates PDFs with transparent text overlays accurately positioned over each word in the document
- **hOCR Integration**: Utilizes hOCR format (HTML-based OCR representation) to maintain precise text positioning
- **Document Quality Improvement**: Makes documents both searchable and selectable while preserving the original appearance
- **Google Document AI Required**: These features rely on Google Document AI's ability to generate hOCR data with accurate word positions

### Local File Saving

paperless-gpt can save both the hOCR files and enhanced PDFs locally:

```yaml
environment:
  # Enable local file saving
  CREATE_LOCAL_HOCR: "true"   # Save hOCR files locally
  CREATE_LOCAL_PDF: "true"    # Save generated PDFs locally
  LOCAL_HOCR_PATH: "/app/hocr" # Path to save hOCR files
  LOCAL_PDF_PATH: "/app/pdf"   # Path to save PDF files
volumes:
  # Mount volumes to access the files from your host
  - ./hocr_files:/app/hocr
  - ./pdf_files:/app/pdf
```

> **Note**: You must mount these directories as volumes in your Docker configuration to access the generated files from your host system.

### PDF Upload to paperless-ngx

Due to limitations in paperless-ngx's API, it's not possible to directly update existing documents with their OCR-enhanced versions. As a workaround, paperless-gpt can:

1. Upload the enhanced PDF as a new document
2. Copy metadata from the original document to the new one
3. Optionally delete the original document

```yaml
environment:
  # PDF upload configuration
  PDF_UPLOAD: "true"          # Upload processed PDFs to paperless-ngx
  PDF_COPY_METADATA: "true"   # Copy metadata from original to new document 
  PDF_REPLACE: "false"        # Whether to delete the original document (use with caution!)
  PDF_OCR_TAGGING: "true"     # Add a tag to mark documents as OCR-processed
  PDF_OCR_COMPLETE_TAG: "paperless-gpt-ocr-complete" # Tag used to mark OCR-processed documents
```

> **⚠️ WARNING ⚠️**  
> Setting `PDF_REPLACE: "true"` will delete the original document after uploading the enhanced version. This process cannot be undone and may result in data loss if something goes wrong during the upload or metadata copying process. Use with extreme caution!

### Metadata Copying Limitations

When copying metadata from the original document to the new one, paperless-gpt attempts to copy:

- Document title
- Tags (including adding the OCR complete tag)
- Correspondent information
- Created date

However, some metadata **cannot** be copied due to paperless-ngx API limitations:

- Document ID (new document always gets a new ID)
- Added date (will reflect the current upload date)
- Modified date
- Custom fields that might be added by other paperless-ngx plugins
- Notes and annotations

### Safety Features

To prevent accidental creation of incomplete documents, paperless-gpt includes several safety features:

1. **Page Count Check**: If using `OCR_LIMIT_PAGES` to process only a subset of pages (for speed or resource reasons), PDF generation will be skipped entirely if fewer pages would be processed than exist in the original document.

```yaml
environment:
  OCR_LIMIT_PAGES: "5"  # Limit OCR to first 5 pages, set to 0 for no limit
```

2. **OCR Complete Tagging**: Documents that have been fully processed with OCR can be automatically tagged with a special tag, preventing duplicate processing.

3. **Processing Skip**: If a document already has the OCR complete tag, processing will be skipped automatically.

### Usage Recommendations

For best results with the enhanced OCR features:

1. **Initial Testing**: Start with `PDF_REPLACE: "false"` until you've confirmed the process works well with your documents.

2. **Regular Backups**: Ensure you have backups of your paperless-ngx database and documents before enabling document replacement.

3. **Process Management**: For large documents, consider using `OCR_LIMIT_PAGES: "0"` to ensure all pages are processed, even though this will take longer.

4. **Local Copies**: Enable local file saving (`CREATE_LOCAL_HOCR` and `CREATE_LOCAL_PDF`) to keep copies of the enhanced files as an extra precaution.

5. **Tagging Strategy**: Use the OCR complete tag (`PDF_OCR_COMPLETE_TAG`) to track which documents have already been processed.

## Configuration

### Environment Variables

> **Note:** When using Ollama, ensure that the Ollama server is running and accessible from the paperless-gpt container.

| Variable                            | Description                                                                                                                                                                               | Required | Default                    |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- | -------------------------- |
| `PAPERLESS_BASE_URL`                | URL of your paperless-ngx instance (e.g. `http://paperless-ngx:8000`).                                                                                                                    | Yes      |                            |
| `PAPERLESS_API_TOKEN`               | API token for paperless-ngx. Generate one in paperless-ngx admin.                                                                                                                         | Yes      |                            |
| `PAPERLESS_PUBLIC_URL`              | Public URL for Paperless (if different from `PAPERLESS_BASE_URL`).                                                                                                                        | No       |                            |
| `MANUAL_TAG`                        | Tag for manual processing.                                                                                                                                                                | No       | paperless-gpt              |
| `AUTO_TAG`                          | Tag for auto processing.                                                                                                                                                                  | No       | paperless-gpt-auto         |
| `LLM_PROVIDER`                      | AI backend (`openai`, `mistral`, or `ollama`).                                                                                                                                            | Yes      |                            |
| `LLM_MODEL`                         | AI model name (e.g., `gpt-4o`, `mistral-large-latest`, `deepseek-r1:8b`).                                                                                                                 | Yes      |                            |
| `OPENAI_API_KEY`                    | OpenAI API key (required if using OpenAI).                                                                                                                                                | Cond.    |                            |
| `MISTRAL_API_KEY`                   | Mistral API key (required if using Mistral).                                                                                                                                              | Cond.    |                            |
| `OPENAI_API_TYPE`                   | Set to `azure` to use Azure OpenAI Service.                                                                                                                                               | No       |                            |
| `OPENAI_BASE_URL`                   | Base URL for OpenAI API. For Azure OpenAI, set to your deployment URL (e.g., `https://your-resource.openai.azure.com`).                                                                   | No       |                            |
| `LLM_LANGUAGE`                      | Likely language for documents (e.g. `English`).                                                                                                                                           | No       | English                    |
| `OLLAMA_HOST`                       | Ollama server URL (e.g. `http://host.docker.internal:11434`).                                                                                                                             | No       |                            |
| `LLM_REQUESTS_PER_MINUTE`           | Maximum requests per minute for the main LLM. Useful for managing API costs or local LLM load.                                                                                            | No       | 120                        |
| `LLM_MAX_RETRIES`                   | Maximum retry attempts for failed main LLM requests.                                                                                                                                      | No       | 3                          |
| `LLM_BACKOFF_MAX_WAIT`              | Maximum wait time between retries for the main LLM (e.g., `30s`).                                                                                                                         | No       | 30s                        |
| `OCR_PROVIDER`                      | OCR provider to use (`llm`, `azure`, or `google_docai`).                                                                                                                                  | No       | llm                        |
| `VISION_LLM_PROVIDER`               | AI backend for LLM OCR (`openai` or `ollama`). Required if OCR_PROVIDER is `llm`.                                                                                                         | Cond.    |                            |
| `VISION_LLM_MODEL`                  | Model name for LLM OCR (e.g. `minicpm-v`). Required if OCR_PROVIDER is `llm`.                                                                                                             | Cond.    |                            |
| `VISION_LLM_REQUESTS_PER_MINUTE`    | Maximum requests per minute for the Vision LLM. Useful for managing API costs or local LLM load.                                                                                          | No       | 120                        |
| `VISION_LLM_MAX_RETRIES`            | Maximum retry attempts for failed Vision LLM requests.                                                                                                                                    | No       | 3                          |
| `VISION_LLM_BACKOFF_MAX_WAIT`       | Maximum wait time between retries for the Vision LLM (e.g., `30s`).                                                                                                                       | No       | 30s                        |
| `AZURE_DOCAI_ENDPOINT`              | Azure Document Intelligence endpoint. Required if OCR_PROVIDER is `azure`.                                                                                                                | Cond.    |                            |
| `AZURE_DOCAI_KEY`                   | Azure Document Intelligence API key. Required if OCR_PROVIDER is `azure`.                                                                                                                 | Cond.    |                            |
| `AZURE_DOCAI_MODEL_ID`              | Azure Document Intelligence model ID. Optional if using `azure` provider.                                                                                                                 | No       | prebuilt-read              |
| `AZURE_DOCAI_TIMEOUT_SECONDS`       | Azure Document Intelligence timeout in seconds.                                                                                                                                           | No       | 120                        |
| `AZURE_DOCAI_OUTPUT_CONTENT_FORMAT` | Azure Document Intelligence output content format. Optional if using `azure` provider. Defaults to `text`. 'markdown' is the other option and it requires the 'prebuild-layout' model ID. | No       | text                       |
| `GOOGLE_PROJECT_ID`                 | Google Cloud project ID. Required if OCR_PROVIDER is `google_docai`.                                                                                                                      | Cond.    |                            |
| `GOOGLE_LOCATION`                   | Google Cloud region (e.g. `us`, `eu`). Required if OCR_PROVIDER is `google_docai`.                                                                                                        | Cond.    |                            |
| `GOOGLE_PROCESSOR_ID`               | Document AI processor ID. Required if OCR_PROVIDER is `google_docai`.                                                                                                                     | Cond.    |                            |
| `GOOGLE_APPLICATION_CREDENTIALS`    | Path to the mounted Google service account key. Required if OCR_PROVIDER is `google_docai`.                                                                                               | Cond.    |                            |
| `DOCLING_URL`                       | URL of the Docling server instance. Required if OCR_PROVIDER is `docling`.                                                                                                                | Cond.    |                            |
| `DOCLING_IMAGE_EXPORT_MODE`         | Mode for image export. Optional; defaults to `embedded` if unset.                                                                                                                         | No       | embedded                   |
| `CREATE_LOCAL_HOCR`                 | Whether to save hOCR files locally.                                                                                                                                                       | No       | false                      |
| `LOCAL_HOCR_PATH`                   | Path where hOCR files will be saved when hOCR generation is enabled.                                                                                                                      | No       | /app/hocr                  |
| `CREATE_LOCAL_PDF`                  | Whether to save enhanced PDFs locally.                                                                                                                                                    | No       | false                      |
| `LOCAL_PDF_PATH`                    | Path where PDF files will be saved when PDF generation is enabled.                                                                                                                        | No       | /app/pdf                   |
| `PDF_UPLOAD`                        | Whether to upload enhanced PDFs to paperless-ngx.                                                                                                                                         | No       | false                      |
| `PDF_REPLACE`                       | Whether to delete the original document after uploading the enhanced version (DANGEROUS).                                                                                                 | No       | false                      |
| `PDF_COPY_METADATA`                 | Whether to copy metadata from the original document to the uploaded PDF. Only applicable when using PDF_UPLOAD.                                                                           | No       | true                       |
| `PDF_OCR_TAGGING`                   | Whether to add a tag to mark documents as OCR-processed.                                                                                                                                 | No       | true                       |
| `PDF_OCR_COMPLETE_TAG`              | Tag used to mark documents as OCR-processed.                                                                                                                                              | No       | paperless-gpt-ocr-complete |
| `AUTO_OCR_TAG`                      | Tag for automatically processing docs with OCR.                                                                                                                                           | No       | paperless-gpt-ocr-auto     |
| `OCR_LIMIT_PAGES`                   | Limit the number of pages for OCR. Set to `0` for no limit.                                                                                                                               | No       | 5                          |
| `LOG_LEVEL`                         | Application log level (`info`, `debug`, `warn`, `error`).                                                                                                                                 | No       | info                       |
| `LISTEN_INTERFACE`                  | Network interface to listen on.                                                                                                                                                           | No       | 8080                       |
| `AUTO_GENERATE_TITLE`               | Generate titles automatically if `paperless-gpt-auto` is used.                                                                                                                            | No       | true                       |
| `AUTO_GENERATE_TAGS`                | Generate tags automatically if `paperless-gpt-auto` is used.                                                                                                                              | No       | true                       |
| `AUTO_GENERATE_CORRESPONDENTS`      | Generate correspondents automatically if `paperless-gpt-auto` is used.                                                                                                                    | No       | true                       |
| `AUTO_GENERATE_CREATED_DATE`        | Generate the created dates automatically if `paperless-gpt-auto` is used.                                                                                                                 | No       | true                       |
| `TOKEN_LIMIT`                       | Maximum tokens allowed for prompts/content. Set to `0` to disable limit. Useful for smaller LLMs.                                                                                         | No       |                            |
| `CORRESPONDENT_BLACK_LIST`          | A comma-separated list of names to exclude from the correspondents suggestions. Example: `John Doe, Jane Smith`.                                                                          | No       |                            |

### Custom Prompt Templates

paperless-gpt's flexible **prompt templates** let you shape how AI responds:

1. **`title_prompt.tmpl`**: For document titles.
2. **`tag_prompt.tmpl`**: For tagging logic.
3. **`ocr_prompt.tmpl`**: For LLM OCR.
4. **`correspondent_prompt.tmpl`**: For correspondent identification.
5. **`created_date_prompt.tmpl`**: For setting of document's created date.

Mount them into your container via:

```yaml
volumes:
  - ./prompts:/app/prompts
```

Then tweak at will—**paperless-gpt** reloads them automatically on startup!

#### Template Variables

Each template has access to specific variables:

**title_prompt.tmpl**:
- `{{.Language}}` - Target language (e.g., "English")
- `{{.Content}}` - Document content text
- `{{.Title}}` - Original document title

**tag_prompt.tmpl**:
- `{{.Language}}` - Target language
- `{{.AvailableTags}}` - List of existing tags in paperless-ngx
- `{{.OriginalTags}}` - Document's current tags
- `{{.Title}}` - Document title
- `{{.Content}}` - Document content text

**ocr_prompt.tmpl**:
- `{{.Language}}` - Target language

**correspondent_prompt.tmpl**:
- `{{.Language}}` - Target language
- `{{.AvailableCorrespondents}}` - List of existing correspondents
- `{{.BlackList}}` - List of blacklisted correspondent names
- `{{.Title}}` - Document title
- `{{.Content}}` - Document content text

**created_date_prompt.tmpl**:
- `{{.Language}}` - Target language
- `{{.Content}}` - Document content text

The templates use Go's text/template syntax. paperless-gpt automatically reloads template changes on startup.

---

## LLM-Based OCR: Compare for Yourself

<details>
<summary>Click to expand the vanilla OCR vs. AI-powered OCR comparison</summary>

### Example 1

**Image**:

![Image](demo/ocr-example1.jpg)

**Vanilla Paperless-ngx OCR**:

```
La Grande Recre

Gentre Gommercial 1'Esplanade
1349 LOLNAIN LA NEWWE
TA BERBOGAAL Tel =. 010 45,96 12
Ticket 1440112 03/11/2006 a 13597:
4007176614518. DINOS. TYRAMNESA
TOTAET.T.LES
ReslE par Lask-Euron
Rencu en Cash Euro
V.14.6 -Hotgese = VALERTE
TICKET A-GONGERVER PORR TONT. EEHANGE
HERET ET A BIENTOT
```

**LLM-Powered OCR (OpenAI gpt-4o)**:

```
La Grande Récré
Centre Commercial l'Esplanade
1348 LOUVAIN LA NEUVE
TVA 860826401 Tel : 010 45 95 12
Ticket 14421 le 03/11/2006 à 15:27:18
4007176614518 DINOS TYRANNOSA 14.90
TOTAL T.T.C. 14.90
Réglé par Cash Euro 50.00
Rendu en Cash Euro 35.10
V.14.6 Hôtesse : VALERIE
TICKET A CONSERVER POUR TOUT ECHANGE
MERCI ET A BIENTOT
```

---

### Example 2

**Image**:

![Image](demo/ocr-example2.jpg)

**Vanilla Paperless-ngx OCR**:

```
Invoice Number: 1-996-84199

Fed: Invoica Date: Sep01, 2014
Accaunt Number: 1334-8037-4
Page: 1012

Fod£x Tax ID 71.0427007

IRISINC
SHARON ANDERSON
4731 W ATLANTIC AVE STE BI
DELRAY BEACH FL 33445-3897 ’ a
Invoice Questions?

Bing, ‚Account Shipping Address: Contact FedEx Reı

ISINC
4731 W ATLANTIC AVE Phone: (800) 622-1147 M-F 7-6 (CST)
DELRAY BEACH FL 33445-3897 US Fax: (800) 548-3020

Internet: www.fedex.com

Invoice Summary Sep 01, 2014

FodEx Ground Services
Other Charges 11.00
Total Charges 11.00 Da £
>
polo) Fz// /G
TOTAL THIS INVOICE .... usps 11.00 P 2/1 f

‘The only charges accrued for this period is the Weekly Service Charge.

The Fedix Ground aceounts teferencedin his involce have been transteired and assigned 10, are owned by,andare payable to FedEx Express:

To onsurs propor credit, plasa raturn this portion wirh your payment 10 FodEx
‚Please do not staple or fold. Ploase make your chack payablı to FedEx.

[TI For change ol address, hc har and camphat lrm or never ide

Remittance Advice
Your payment is due by Sep 16, 2004

Number Number Dus

1334803719968 41993200000110071

AT 01 0391292 468448196 A**aDGT

IRISINC Illallun elalalssollallansdHilalellund
SHARON ANDERSON

4731 W ATLANTIC AVE STEBI FedEx

DELRAY BEACH FL 334453897 PO. Box 94516

PALATINE IL 60094-4515
```

**LLM-Powered OCR (OpenAI gpt-4o)**:

```
FedEx.                                                                                      Invoice Number: 1-996-84199
                                                                                           Invoice Date: Sep 01, 2014
                                                                                           Account Number: 1334-8037-4
                                                                                           Page: 1 of 2
                                                                                           FedEx Tax ID: 71-0427007

I R I S INC
SHARON ANDERSON
4731 W ATLANTIC AVE STE B1
DELRAY BEACH FL 33445-3897
                                                                                           Invoice Questions?
Billing Account Shipping Address:                                                          Contact FedEx Revenue Services
I R I S INC                                                                                Phone: (800) 622-1147 M-F 7-6 (CST)
4731 W ATLANTIC AVE                                                                        Fax: (800) 548-3020
DELRAY BEACH FL 33445-3897 US                                                              Internet: www.fedex.com

Invoice Summary Sep 01, 2014

FedEx Ground Services
Other Charges                                                                 11.00

Total Charges .......................................................... USD $          11.00

TOTAL THIS INVOICE .............................................. USD $                 11.00

The only charges accrued for this period is the Weekly Service Charge.

                                                                                           RECEIVED
                                                                                           SEP _ 8 REC'D
                                                                                           BY: _

                                                                                           posted 9/21/14

The FedEx Ground accounts referenced in this invoice have been transferred and assigned to, are owned by, and are payable to FedEx Express.

To ensure proper credit, please return this portion with your payment to FedEx.
Please do not staple or fold. Please make your check payable to FedEx.

❑ For change of address, check here and complete form on reverse side.

Remittance Advice
Your payment is due by Sep 16, 2004

Invoice
Number
1-996-84199

Account
Number
1334-8037-4

Amount
Due
USD $ 11.00

133480371996841993200000110071

AT 01 031292 468448196 A**3DGT

I R I S INC
SHARON ANDERSON
4731 W ATLANTIC AVE STE B1
DELRAY BEACH FL 33445-3897

FedEx
P.O. Box 94515
```

---

</details>

**Why Does It Matter?**

- Traditional OCR often jumbles text from complex or low-quality scans.
- Large Language Models interpret context and correct likely errors, producing results that are more precise and readable.
- You can integrate these cleaned-up texts into your **paperless-ngx** pipeline for better tagging, searching, and archiving.

### How It Works

- **Vanilla OCR** typically uses classical methods or Tesseract-like engines to extract text, which can result in garbled outputs for complex fonts or poor-quality scans.
- **LLM-Powered OCR** uses your chosen AI backend—OpenAI or Ollama—to interpret the image's text in a more context-aware manner. This leads to fewer errors and more coherent text.
- **Google Document AI and Azure Document Intelligence** provide high-accuracy OCR with advanced layout analysis.
- **Enhanced PDF Generation** combines OCR results with the original document to create searchable PDFs with properly positioned text layers.

---

## Usage

1. **Tag Documents**

   - Add `paperless-gpt` tag to documents for manual processing
   - Add `paperless-gpt-auto` for automatic processing
   - Add `paperless-gpt-ocr-auto` for automatic OCR processing

2. **Visit Web UI**

   - Go to `http://localhost:8080` (or your host) in your browser
   - Review documents tagged for processing

3. **Generate & Apply Suggestions**

   - Click "Generate Suggestions" to see AI-proposed titles/tags/correspondents
   - Review and approve or edit suggestions
   - Click "Apply" to save changes to paperless-ngx

4. **OCR Processing**
   - Tag documents with appropriate OCR tag to process them
   - If enhanced PDF features are enabled, documents will be processed accordingly:
     - For local file saving, check the configured directories for output files
     - For PDF uploads, new documents will appear in paperless-ngx with copied metadata
   - Monitor progress in the Web UI
   - Review results and apply changes

## Troubleshooting

### Working with Local LLMs

When using local LLMs (like those through Ollama), you might need to adjust certain settings to optimize performance:

#### Token Management

- Use `TOKEN_LIMIT` environment variable to control the maximum number of tokens sent to the LLM
- Smaller models might truncate content unexpectedly if given too much text
- Start with a conservative limit (e.g., 1000 tokens) and adjust based on your model's capabilities
- Set to `0` to disable the limit (use with caution)

Example configuration for smaller models:

```yaml
environment:
  TOKEN_LIMIT: "2000" # Adjust based on your model's context window
  LLM_PROVIDER: "ollama"
  LLM_MODEL: "deepseek-r1:8b" # Or other local model
```

Common issues and solutions:

- If you see truncated or incomplete responses, try lowering the `TOKEN_LIMIT`
- If processing is too limited, gradually increase the limit while monitoring performance
- For models with larger context windows, you can increase the limit or disable it entirely

### PDF Processing Issues

- If PDFs aren't being generated, check that `OCR_LIMIT_PAGES` isn't set too low compared to your document page count
- Ensure volumes are properly mounted if using `CREATE_LOCAL_PDF` or `CREATE_LOCAL_HOCR`
- When using `PDF_REPLACE: "true"`, verify you have recent backups of your paperless-ngx data

---

## Contributing

**Pull requests** and **issues** are welcome!

1. Fork the repo
2. Create a branch (`feature/my-awesome-update`)
3. Commit changes (`git commit -m "Improve X"`)
4. Open a PR

Check out our [contributing guidelines](CONTRIBUTING.md) for details.

---

## Support the Project

If paperless-gpt is saving you time and making your document management easier, please consider supporting its continued development:

- **[GitHub Sponsors](https://github.com/sponsors/icereed)**: Help fund ongoing development and maintenance
- **Share** your success stories and use cases
- **Star** the project on GitHub
- **Contribute** code, documentation, or bug reports

Your support helps ensure paperless-gpt remains actively maintained and continues to improve!

---

## License

paperless-gpt is licensed under the [MIT License](LICENSE). Feel free to adapt and share!

---

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=icereed/paperless-gpt&type=Date)](https://www.star-history.com/#icereed/paperless-gpt&Date)

---

## Disclaimer

This project is **not** officially affiliated with [paperless-ngx][paperless-ngx]. Use at your own risk.

---

**paperless-gpt**: The **LLM-based** companion your doc management has been waiting for. Enjoy effortless, intelligent document titles, tags, and next-level OCR.

[paperless-ngx]: https://github.com/paperless-ngx/paperless-ngx
[docker-install]: https://docs.docker.com/get-docker/

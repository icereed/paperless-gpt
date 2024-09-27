# paperless-gpt

[![License](https://img.shields.io/github/license/icereed/paperless-gpt)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/icereed/paperless-gpt)](https://hub.docker.com/r/icereed/paperless-gpt)

**paperless-gpt** is a tool designed to generate accurate and meaningful document titles for [paperless-ngx](https://github.com/paperless-ngx/paperless-ngx) using Large Language Models (LLMs). It supports multiple LLM providers, including **OpenAI** and **Ollama**. With paperless-gpt, you can streamline your document management by automatically suggesting appropriate titles and tags based on the content of your scanned documents.

[![Demo](./demo.gif)](./demo.gif)

## Features

- **Multiple LLM Support**: Choose between OpenAI and Ollama for generating document titles.
- **Easy Integration**: Works seamlessly with your existing paperless-ngx setup.
- **User-Friendly Interface**: Intuitive web interface for reviewing and applying suggested titles.
- **Dockerized Deployment**: Simple setup using Docker and Docker Compose.

## Table of Contents

- [paperless-gpt](#paperless-gpt)
  - [Features](#features)
  - [Table of Contents](#table-of-contents)
  - [Getting Started](#getting-started)
    - [Prerequisites](#prerequisites)
    - [Installation](#installation)
      - [Docker Compose](#docker-compose)
      - [Manual Setup](#manual-setup)
  - [Configuration](#configuration)
    - [Environment Variables](#environment-variables)
  - [Usage](#usage)
  - [Contributing](#contributing)
  - [License](#license)

## Getting Started

### Prerequisites

- [Docker](https://www.docker.com/get-started) installed on your system.
- A running instance of [paperless-ngx](https://github.com/paperless-ngx/paperless-ngx).
- Access to an LLM provider:
  - **OpenAI**: An API key with access to models like `gpt-4o` or `gpt-3.5-turbo`.
  - **Ollama**: A running Ollama server with models like `llama2` installed.

### Installation

#### Docker Compose

The easiest way to get started is by using Docker Compose. Below is an example `docker-compose.yml` file to set up paperless-gpt alongside paperless-ngx.

```yaml
version: '3.7'
services:
  paperless-ngx:
    image: ghcr.io/paperless-ngx/paperless-ngx:latest
    # ... (your existing paperless-ngx configuration)

  paperless-gpt:
    image: icereed/paperless-gpt:latest
    environment:
      PAPERLESS_BASE_URL: 'http://paperless-ngx:8000'
      PAPERLESS_API_TOKEN: 'your_paperless_api_token'
      LLM_PROVIDER: 'openai' # or 'ollama'
      LLM_MODEL: 'gpt-4o'     # or 'llama2'
      OPENAI_API_KEY: 'your_openai_api_key' # Required if using OpenAI
      LLM_LANGUAGE: 'English' # Optional, default is 'English'
      OLLAMA_HOST: http://host.docker.internal:11434 # Useful if using Ollama
    ports:
      - '8080:8080'
    depends_on:
      - paperless-ngx
```

**Note:** Replace the placeholder values with your actual configuration.

#### Manual Setup

If you prefer to run the application manually:

1. **Clone the Repository:**

   ```bash
   git clone https://github.com/icereed/paperless-gpt.git
   cd paperless-gpt
   ```

2. **Build the Docker Image:**

   ```bash
   docker build -t paperless-gpt .
   ```

3. **Run the Container:**

   ```bash
   docker run -d \
     -e PAPERLESS_BASE_URL='http://your_paperless_ngx_url' \
     -e PAPERLESS_API_TOKEN='your_paperless_api_token' \
     -e LLM_PROVIDER='openai' \
     -e LLM_MODEL='gpt-4o' \
     -e OPENAI_API_KEY='your_openai_api_key' \
     -e LLM_LANGUAGE='English' \
     -p 8080:8080 \
     paperless-gpt
   ```

## Configuration

### Environment Variables

| Variable              | Description                                                                                         | Required |
|-----------------------|-----------------------------------------------------------------------------------------------------|----------|
| `PAPERLESS_BASE_URL`  | The base URL of your paperless-ngx instance (e.g., `http://paperless-ngx:8000`).                   | Yes      |
| `PAPERLESS_API_TOKEN` | API token for accessing paperless-ngx. You can generate one in the paperless-ngx admin interface.   | Yes      |
| `LLM_PROVIDER`        | The LLM provider to use (`openai` or `ollama`).                                                     | Yes      |
| `LLM_MODEL`           | The model name to use (e.g., `gpt-4`, `gpt-3.5-turbo`, `llama2`).                                  | Yes      |
| `OPENAI_API_KEY`      | Your OpenAI API key. Required if using OpenAI as the LLM provider.                                  | Cond.    |
| `LLM_LANGUAGE`        | The likely language of your documents (e.g., `English`, `German`). Default is `English`.            | No       |
| `OLLAMA_HOST`         | The URL of the Ollama server (e.g., `http://host.docker.internal:11434`). Useful if using Ollama. Default is `http://127.0.0.1:11434`  | No    |

**Note:** When using Ollama, ensure that the Ollama server is running and accessible from the paperless-gpt container.

## Usage

1. **Tag Documents in paperless-ngx:**

   - Add the tag `paperless-gpt` to documents you want to process. This tag is configurable via the `tagToFilter` variable in the code (default is `paperless-gpt`).

2. **Access the paperless-gpt Interface:**

   - Open your browser and navigate to `http://localhost:8080`.

3. **Process Documents:**

   - Click on **"Generate Suggestions"** to let the LLM generate title suggestions based on the document content.

4. **Review and Apply Titles and Tags:**

   - Review the suggested titles. You can edit them if necessary.
   - Click on **"Apply Suggestions"** to update the document titles in paperless-ngx.

## Contributing

Contributions are welcome! Please read the [contributing guidelines](CONTRIBUTING.md) before submitting a pull request.

1. **Fork the Repository**

2. **Create a Feature Branch**

   ```bash
   git checkout -b feature/my-new-feature
   ```

3. **Commit Your Changes**

   ```bash
   git commit -am 'Add some feature'
   ```

4. **Push to the Branch**

   ```bash
   git push origin feature/my-new-feature
   ```

5. **Create a Pull Request**

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

**Disclaimer:** This project is not affiliated with the official paperless-ngx project. Use at your own discretion.

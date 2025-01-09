# paperless-gpt
[![License](https://img.shields.io/github/license/icereed/paperless-gpt)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/icereed/paperless-gpt)](https://hub.docker.com/r/icereed/paperless-gpt)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)

![Screenshot](./paperless-gpt-screenshot.png)

**paperless-gpt** seamlessly pairs with [paperless-ngx][paperless-ngx] to generate **AI-powered document titles** and **tags**, saving you hours of manual sorting. While other tools may offer AI chat features, **paperless-gpt** stands out by **supercharging OCR with LLMs**—ensuring high accuracy, even with tricky scans. If you’re craving next-level text extraction and effortless document organization, this is your solution.

https://github.com/user-attachments/assets/bd5d38b9-9309-40b9-93ca-918dfa4f3fd4

---

## Key Highlights

1. **LLM-Enhanced OCR**  
   Harness Large Language Models (OpenAI or Ollama) for **better-than-traditional** OCR—turn messy or low-quality scans into context-aware, high-fidelity text.

2. **Automatic Title & Tag Generation**  
   No more guesswork. Let the AI do the naming and categorizing. You can easily review suggestions and refine them if needed.

3. **Extensive Customization**  
   - **Prompt Templates**: Tweak your AI prompts to reflect your domain, style, or preference.  
   - **Tagging**: Decide how documents get tagged—manually, automatically, or via OCR-based flows.

4. **Simple Docker Deployment**  
   A few environment variables, and you’re off! Compose it alongside paperless-ngx with minimal fuss.

5. **Unified Web UI**  
   - **Manual Review**: Approve or tweak AI’s suggestions.  
   - **Auto Processing**: Focus only on edge cases while the rest is sorted for you.

6. **Opt-In LLM-based OCR**  
   If you opt in, your images get read by a Vision LLM, pushing boundaries beyond standard OCR tools.

---

## Table of Contents
- [Key Highlights](#key-highlights)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
    - [Docker Compose](#docker-compose)
    - [Manual Setup](#manual-setup)
- [Configuration](#configuration)
  - [Environment Variables](#environment-variables)
  - [Custom Prompt Templates](#custom-prompt-templates)
    - [Prompt Templates Directory](#prompt-templates-directory)
    - [Mounting the Prompts Directory](#mounting-the-prompts-directory)
    - [Editing the Prompt Templates](#editing-the-prompt-templates)
    - [Template Syntax and Variables](#template-syntax-and-variables)
- [Usage](#usage)
- [Contributing](#contributing)
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
  - **Ollama**: A running Ollama server with models like `llama2`.

### Installation

#### Docker Compose

Here’s an example `docker-compose.yml` to spin up **paperless-gpt** alongside paperless-ngx:

```yaml
version: '3.7'
services:
  paperless-ngx:
    image: ghcr.io/paperless-ngx/paperless-ngx:latest
    # ... (your existing paperless-ngx config)

  paperless-gpt:
    image: icereed/paperless-gpt:latest
    environment:
      PAPERLESS_BASE_URL: 'http://paperless-ngx:8000'
      PAPERLESS_API_TOKEN: 'your_paperless_api_token'
      PAPERLESS_PUBLIC_URL: 'http://paperless.mydomain.com' # Optional
      MANUAL_TAG: 'paperless-gpt'          # Optional, default: paperless-gpt
      AUTO_TAG: 'paperless-gpt-auto'       # Optional, default: paperless-gpt-auto
      LLM_PROVIDER: 'openai'               # or 'ollama'
      LLM_MODEL: 'gpt-4o'                  # or 'llama2'
      OPENAI_API_KEY: 'your_openai_api_key'
      LLM_LANGUAGE: 'English'              # Optional, default: English
      OLLAMA_HOST: 'http://host.docker.internal:11434' # If using Ollama
      VISION_LLM_PROVIDER: 'ollama'        # (for OCR) - openai or ollama
      VISION_LLM_MODEL: 'minicpm-v'        # (for OCR) - minicpm-v (ollama example), gpt-4o (for openai), etc.
      AUTO_OCR_TAG: 'paperless-gpt-ocr-auto' # Optional, default: paperless-gpt-ocr-auto
      LOG_LEVEL: 'info'                    # Optional: debug, warn, error
    volumes:
      - ./prompts:/app/prompts   # Mount the prompts directory
    ports:
      - '8080:8080'
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

## Configuration

### Environment Variables

| Variable               | Description                                                                                                      | Required |
|------------------------|------------------------------------------------------------------------------------------------------------------|----------|
| `PAPERLESS_BASE_URL`   | URL of your paperless-ngx instance (e.g. `http://paperless-ngx:8000`).                                          | Yes      |
| `PAPERLESS_API_TOKEN`  | API token for paperless-ngx. Generate one in paperless-ngx admin.                                               | Yes      |
| `PAPERLESS_PUBLIC_URL` | Public URL for Paperless (if different from `PAPERLESS_BASE_URL`).                                              | No       |
| `MANUAL_TAG`           | Tag for manual processing. Default: `paperless-gpt`.                                                            | No       |
| `AUTO_TAG`             | Tag for auto processing. Default: `paperless-gpt-auto`.                                                         | No       |
| `LLM_PROVIDER`         | AI backend (`openai` or `ollama`).                                                                              | Yes      |
| `LLM_MODEL`            | AI model name, e.g. `gpt-4o`, `gpt-3.5-turbo`, `llama2`.                                                         | Yes      |
| `OPENAI_API_KEY`       | OpenAI API key (required if using OpenAI).                                                                      | Cond.    |
| `LLM_LANGUAGE`         | Likely language for documents (e.g. `English`). Default: `English`.                                             | No       |
| `OLLAMA_HOST`          | Ollama server URL (e.g. `http://host.docker.internal:11434`).                                                   | No       |
| `VISION_LLM_PROVIDER`  | AI backend for OCR (`openai` or `ollama`).                                                                      | No       |
| `VISION_LLM_MODEL`     | Model name for OCR (e.g. `minicpm-v`).                                                                          | No       |
| `AUTO_OCR_TAG`         | Tag for automatically processing docs with OCR. Default: `paperless-gpt-ocr-auto`.                              | No       |
| `LOG_LEVEL`            | Application log level (`info`, `debug`, `warn`, `error`). Default: `info`.                                      | No       |
| `LISTEN_INTERFACE`     | Network interface to listen on. Default: `:8080`.                                                               | No       |
| `WEBUI_PATH`           | Path for static content. Default: `./web-app/dist`.                                                             | No       |
| `AUTO_GENERATE_TITLE`  | Generate titles automatically if `paperless-gpt-auto` is used. Default: `true`.                                  | No       |
| `AUTO_GENERATE_TAGS`   | Generate tags automatically if `paperless-gpt-auto` is used. Default: `true`.                                   | No       |

### Custom Prompt Templates

paperless-gpt’s flexible **prompt templates** let you shape how AI responds:

1. **`title_prompt.tmpl`**: For document titles.  
2. **`tag_prompt.tmpl`**: For tagging logic.
3. **`ocr_prompt.tmpl`**: For LLM OCR.

Mount them into your container via:

```yaml
  volumes:
    - ./prompts:/app/prompts
```

Then tweak at will—**paperless-gpt** reloads them automatically on startup!

---

## Usage

1. **Tag Documents**  
   - Add `paperless-gpt` or your custom tag to the docs you want to AI-ify.

2. **Visit Web UI**  
   - Go to `http://localhost:8080` (or your host) in your browser.

3. **Generate & Apply Suggestions**  
   - Click “Generate Suggestions” to see AI-proposed titles/tags.
   - Approve, edit, or discard. Hit “Apply” to finalize in paperless-ngx.

4. **Try LLM-Based OCR (Experimental)**  
   - If you enabled `VISION_LLM_PROVIDER` and `VISION_LLM_MODEL`, let AI-based OCR read your scanned PDFs.  
   - Tag those documents with `paperless-gpt-ocr-auto` (or your custom `AUTO_OCR_TAG`).

**Tip**: The entire pipeline can be **fully automated** if you prefer minimal manual intervention.

---

## Contributing

**Pull requests** and **issues** are welcome!  
1. Fork the repo  
2. Create a branch (`feature/my-awesome-update`)  
3. Commit changes (`git commit -m "Improve X"`)  
4. Open a PR

Check out our [contributing guidelines](CONTRIBUTING.md) for details.

---

## License

paperless-gpt is licensed under the [MIT License](LICENSE). Feel free to adapt and share!

---

## Star History
[![Star History Chart](https://api.star-history.com/svg?repos=icereed/paperless-gpt&type=Date)](https://star-history.com/#icereed/paperless-gpt&Date)

---

## Disclaimer
This project is **not** officially affiliated with [paperless-ngx][paperless-ngx]. Use at your own risk.

---

**paperless-gpt**: The **LLM-based** companion your doc management has been waiting for. Enjoy effortless, intelligent document titles, tags, and next-level OCR.

[paperless-ngx]: https://github.com/paperless-ngx/paperless-ngx
[docker-install]: https://docs.docker.com/get-docker/

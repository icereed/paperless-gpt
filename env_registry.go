package main

// EnvVar describes one environment variable paperless-gpt understands. The
// registry is the single structured source of truth for the /api/config
// diagnostics view; a drift test (env_registry_test.go) fails the build if the
// code reads an os.Getenv key that is missing here, so this list can never
// silently fall behind the code.
//
// NOTE: the human-facing README env table is currently maintained separately.
// Generating it from this registry is a documented follow-up (see
// docs/adr/0002).
type EnvVar struct {
	Name        string
	Category    string
	Secret      bool // value is a credential — never emit the value, only whether it is set
	Default     string
	Description string
}

// envCategoryOrder is the display order for grouping in the UI.
var envCategoryOrder = []string{
	"Connection",
	"LLM",
	"OCR",
	"PDF & hOCR",
	"Tags & automation",
	"Processing & limits",
	"Server & container",
}

// envRegistry lists every environment variable paperless-gpt reads, plus a few
// consumed by the container entrypoint / cloud SDKs (PUID, PGID,
// GOOGLE_APPLICATION_CREDENTIALS) that never appear as os.Getenv calls in Go.
var envRegistry = []EnvVar{
	{Name: "ANTHROPIC_API_KEY", Category: "LLM", Secret: true, Default: "", Description: "Anthropic API key (required if using Anthropic/Claude)."},
	{Name: "AUTO_GENERATE_CORRESPONDENTS", Category: "Tags & automation", Secret: false, Default: "true", Description: "Generate correspondents automatically if `paperless-gpt-auto` is used."},
	{Name: "AUTO_GENERATE_CREATED_DATE", Category: "Tags & automation", Secret: false, Default: "true", Description: "Generate the created dates automatically if `paperless-gpt-auto` is used."},
	{Name: "AUTO_GENERATE_DOCUMENT_TYPE", Category: "Tags & automation", Secret: false, Default: "true", Description: "Generate document types automatically if `paperless-gpt-auto` is used. Only existing document types from paperless-ngx will be used."},
	{Name: "AUTO_GENERATE_TAGS", Category: "Tags & automation", Secret: false, Default: "true", Description: "Generate tags automatically if `paperless-gpt-auto` is used."},
	{Name: "AUTO_GENERATE_TITLE", Category: "Tags & automation", Secret: false, Default: "true", Description: "Generate titles automatically if `paperless-gpt-auto` is used."},
	{Name: "AUTO_OCR_TAG", Category: "Tags & automation", Secret: false, Default: "paperless-gpt-ocr-auto", Description: "Tag for automatically processing docs with OCR."},
	{Name: "AUTO_TAG", Category: "Tags & automation", Secret: false, Default: "paperless-gpt-auto", Description: "Tag for auto processing."},
	{Name: "AUTO_TAG_COMPLETE", Category: "Tags & automation", Secret: false, Default: "paperless-gpt-auto-complete", Description: "Tag added to documents after auto-processing is complete. Only applied during auto-processing, not manual review. Set to an empty string (`AUTO_TAG_COMPLETE=\"\"`) to disable. When the variable is unset, the default tag is used."},
	{Name: "AZURE_DOCAI_ENDPOINT", Category: "OCR", Secret: false, Default: "", Description: "Azure Document Intelligence endpoint. Required if OCR_PROVIDER is `azure`."},
	{Name: "AZURE_DOCAI_KEY", Category: "OCR", Secret: true, Default: "", Description: "Azure Document Intelligence API key. Required if OCR_PROVIDER is `azure`."},
	{Name: "AZURE_DOCAI_MODEL_ID", Category: "OCR", Secret: false, Default: "prebuilt-read", Description: "Azure Document Intelligence model ID. Optional if using `azure` provider."},
	{Name: "AZURE_DOCAI_OUTPUT_CONTENT_FORMAT", Category: "OCR", Secret: false, Default: "text", Description: "Azure Document Intelligence output content format. Optional if using `azure` provider. Defaults to `text`. 'markdown' is the other option and it requires the 'prebuild-layout' model ID."},
	{Name: "AZURE_DOCAI_TIMEOUT_SECONDS", Category: "OCR", Secret: false, Default: "120", Description: "Azure Document Intelligence timeout in seconds."},
	{Name: "CORRESPONDENT_BLACK_LIST", Category: "Processing & limits", Secret: false, Default: "", Description: "A comma-separated list of names to exclude from the correspondents suggestions. Example: `John Doe, Jane Smith`."},
	{Name: "CREATE_LOCAL_HOCR", Category: "PDF & hOCR", Secret: false, Default: "false", Description: "Whether to save hOCR files locally."},
	{Name: "CREATE_LOCAL_PDF", Category: "PDF & hOCR", Secret: false, Default: "false", Description: "Whether to save enhanced PDFs locally."},
	{Name: "CREATE_NEW_TAGS", Category: "Tags & automation", Secret: false, Default: "false", Description: "Allow the LLM to suggest new tags that don't exist in paperless-ngx yet. When enabled, new tags will be created automatically in paperless-ngx."},
	{Name: "DOCLING_IMAGE_EXPORT_MODE", Category: "OCR", Secret: false, Default: "embedded", Description: "Mode for image export. Optional; defaults to `embedded` if unset."},
	{Name: "DOCLING_OCR_ENGINE", Category: "OCR", Secret: false, Default: "easyocr", Description: "Sets the ocr engine, if `DOCLING_OCR_PIPELINE` is set to `standard`. Optional; defaults to `easyocr`"},
	{Name: "DOCLING_OCR_PIPELINE", Category: "OCR", Secret: false, Default: "vlm", Description: "Sets the pipeline type. Optional; defaults to `vlm` if unset."},
	{Name: "DOCLING_URL", Category: "OCR", Secret: false, Default: "", Description: "URL of the Docling server instance. Required if OCR_PROVIDER is `docling`."},
	{Name: "FAIL_TAG", Category: "Tags & automation", Secret: false, Default: "paperless-gpt-failed", Description: "Tag applied to a document when paperless-gpt could not apply the full LLM suggestion. Two cases trigger it: (1) **partial success** — paperless-ngx rejected one or more fields (e.g. an LLM-suggested date in an impossible format such as `2023-01-79`); paperless-gpt drops the rejected fields, retries the update with the rest, and applies this tag so the user knows the document needs review; (2) **hard failure** — the update could not be salvaged; paperless-gpt removes the auto tag (to break the processing loop) and applies this tag. The tag is created automatically in paperless-ngx at startup if it does not exist."},
	{Name: "GOOGLEAI_API_KEY", Category: "LLM", Secret: true, Default: "", Description: "Google Gemini API key (required if using `LLM_PROVIDER=googleai`)."},
	{Name: "GOOGLEAI_THINKING_BUDGET", Category: "LLM", Secret: false, Default: "", Description: "(Optional, googleai only) Integer. Controls Gemini \"thinking\" budget. If unset, model default is used (thinking enabled if supported). Set to `0` to disable thinking (if model supports it)."},
	{Name: "GOOGLE_APPLICATION_CREDENTIALS", Category: "OCR", Secret: false, Default: "", Description: "Path to the mounted Google service account key. Required if OCR_PROVIDER is `google_docai`."},
	{Name: "GOOGLE_LOCATION", Category: "OCR", Secret: false, Default: "", Description: "Google Cloud region (e.g. `us`, `eu`). Required if OCR_PROVIDER is `google_docai`."},
	{Name: "GOOGLE_PROCESSOR_ID", Category: "OCR", Secret: false, Default: "", Description: "Document AI processor ID. Required if OCR_PROVIDER is `google_docai`."},
	{Name: "GOOGLE_PROJECT_ID", Category: "OCR", Secret: false, Default: "", Description: "Google Cloud project ID. Required if OCR_PROVIDER is `google_docai`."},
	{Name: "IMAGE_MAX_FILE_BYTES", Category: "Processing & limits", Secret: false, Default: "10485760", Description: "Maximum JPEG file size in bytes for rendered page images. Images exceeding this are compressed or resized."},
	{Name: "IMAGE_MAX_PIXEL_DIMENSION", Category: "Processing & limits", Secret: false, Default: "10000", Description: "Maximum pixels along any side when rendering document pages to images."},
	{Name: "IMAGE_MAX_RENDER_DPI", Category: "Processing & limits", Secret: false, Default: "600", Description: "Maximum DPI used when rendering document pages to images."},
	{Name: "IMAGE_MAX_TOTAL_PIXELS", Category: "Processing & limits", Secret: false, Default: "40000000", Description: "Maximum total pixel count (width × height) when rendering document pages to images."},
	{Name: "LISTEN_INTERFACE", Category: "Server & container", Secret: false, Default: "8080", Description: "Network interface to listen on."},
	{Name: "LLM_BACKOFF_MAX_WAIT", Category: "LLM", Secret: false, Default: "30s", Description: "Maximum wait time between retries for the main LLM (e.g., `30s`)."},
	{Name: "LLM_LANGUAGE", Category: "Processing & limits", Secret: false, Default: "English", Description: "Likely language for documents (e.g. `English`). Appears in the prompt to help the LLM."},
	{Name: "LLM_MAX_RETRIES", Category: "LLM", Secret: false, Default: "3", Description: "Maximum retry attempts for failed main LLM requests."},
	{Name: "LLM_MODEL", Category: "LLM", Secret: false, Default: "", Description: "AI model name (e.g., `gpt-4o`, `mistral-large-latest`, `qwen3:8b`, `claude-sonnet-4-5`)."},
	{Name: "LLM_PROVIDER", Category: "LLM", Secret: false, Default: "", Description: "AI backend (`openai`, `ollama`, `googleai`, `mistral`, or `anthropic`)."},
	{Name: "LLM_REQUESTS_PER_MINUTE", Category: "LLM", Secret: false, Default: "120", Description: "Maximum requests per minute for the main LLM. Useful for managing API costs or local LLM load."},
	{Name: "LOCAL_HOCR_PATH", Category: "PDF & hOCR", Secret: false, Default: "/app/hocr", Description: "Path where hOCR files will be saved when hOCR generation is enabled."},
	{Name: "LOCAL_PDF_PATH", Category: "PDF & hOCR", Secret: false, Default: "/app/pdf", Description: "Path where PDF files will be saved when PDF generation is enabled."},
	{Name: "LOG_LEVEL", Category: "Server & container", Secret: false, Default: "info", Description: "Application log level (`info`, `debug`, `warn`, `error`)."},
	{Name: "MANUAL_TAG", Category: "Tags & automation", Secret: false, Default: "paperless-gpt", Description: "Tag for manual processing."},
	{Name: "MISTRAL_API_KEY", Category: "LLM", Secret: true, Default: "", Description: "Mistral API key (required if using Mistral)."},
	{Name: "MISTRAL_MODEL", Category: "OCR", Secret: false, Default: "mistral-ocr-latest", Description: "Mistral OCR model used when OCR_PROVIDER is mistral_ocr."},
	{Name: "OCR_LIMIT_PAGES", Category: "OCR", Secret: false, Default: "5", Description: "Limit the number of pages for OCR. Set to `0` for no limit. Not applied in `whole_pdf` mode (see Whole PDF Mode), which always processes the entire document."},
	{Name: "OCR_PROCESS_MODE", Category: "OCR", Secret: false, Default: "image", Description: "Method for processing documents: `image` (convert to images first), `pdf` (process PDF pages directly), or `whole_pdf` (entire PDF at once)."},
	{Name: "OCR_PROVIDER", Category: "OCR", Secret: false, Default: "llm", Description: "OCR provider to use (`llm`, `azure`, or `google_docai`)."},
	{Name: "OLLAMA_CONTEXT_LENGTH", Category: "LLM", Secret: false, Default: "", Description: "(Ollama only) Integer. Sets NumCtx (context window) for the Ollama runner. If unset or 0, the model default is used."},
	{Name: "OLLAMA_HEADERS", Category: "LLM", Secret: false, Default: "", Description: "(Ollama only) Comma-separated `Key=Value` pairs added as HTTP headers to every Ollama request. Useful for authorization when Ollama is behind a reverse proxy (e.g. `Authorization=Bearer mytoken`)."},
	{Name: "OLLAMA_HOST", Category: "LLM", Secret: false, Default: "", Description: "Ollama server URL (e.g. `http://host.docker.internal:11434`)."},
	{Name: "OLLAMA_OCR_TOP_K", Category: "OCR", Secret: false, Default: "", Description: "(Ollama only) Top-k token sampling for Vision OCR. Lower favors more likely tokens; higher increases diversity."},
	{Name: "OLLAMA_THINK", Category: "LLM", Secret: false, Default: "", Description: "(Ollama only) Set to `false` to disable Ollama reasoning (\"think\") mode, `true` to force it on. Recommended `false` for thinking-mode models (e.g. Qwen 3, Gemma 3) on tasks that need strict output formats — with thinking left on, these models can spend the whole token budget reasoning and return empty content. If unset, the model's default applies."},
	{Name: "OPENAI_API_KEY", Category: "LLM", Secret: true, Default: "", Description: "OpenAI API key (required if using OpenAI)."},
	{Name: "OPENAI_API_TYPE", Category: "LLM", Secret: false, Default: "", Description: "Set to `azure` to use Azure OpenAI Service."},
	{Name: "OPENAI_BASE_URL", Category: "LLM", Secret: false, Default: "", Description: "Base URL for OpenAI API. Use it to point to an OpenAI-compatible endpoint (e.g. OpenRouter, LiteLLM, vLLM). For Azure OpenAI, set to your deployment URL (e.g., `https://your-resource.openai.azure.com`)."},
	{Name: "PAPERLESS_API_TOKEN", Category: "Connection", Secret: true, Default: "", Description: "API token for paperless-ngx. Generate one in paperless-ngx admin."},
	{Name: "PAPERLESS_BASE_URL", Category: "Connection", Secret: false, Default: "", Description: "URL of your paperless-ngx instance (e.g. `http://paperless-ngx:8000`)."},
	{Name: "PAPERLESS_GPT_CACHE_DIR", Category: "Connection", Secret: false, Default: "OS temp directory", Description: "Base directory for the page-image cache (rendered previews and OCR page images)."},
	{Name: "PAPERLESS_INSECURE_SKIP_VERIFY", Category: "Connection", Secret: false, Default: "false", Description: "Set to true to skip TLS certificate verification when talking to paperless-ngx. Only for self-signed setups; weakens transport security."},
	{Name: "PAPERLESS_PUBLIC_URL", Category: "Connection", Secret: false, Default: "", Description: "Public URL for Paperless (if different from `PAPERLESS_BASE_URL`)."},
	{Name: "PDF_COPY_METADATA", Category: "PDF & hOCR", Secret: false, Default: "true", Description: "Whether to copy metadata from the original document to the uploaded PDF. Only applicable when using PDF_UPLOAD."},
	{Name: "PDF_OCR_COMPLETE_TAG", Category: "PDF & hOCR", Secret: false, Default: "paperless-gpt-ocr-complete", Description: "Tag used to mark documents as OCR-processed."},
	{Name: "PDF_OCR_TAGGING", Category: "PDF & hOCR", Secret: false, Default: "true", Description: "Whether to add a tag to mark documents as OCR-processed."},
	{Name: "PDF_REPLACE", Category: "PDF & hOCR", Secret: false, Default: "false", Description: "Whether to delete the original document after uploading the enhanced version (DANGEROUS)."},
	{Name: "PDF_SKIP_EXISTING_OCR", Category: "PDF & hOCR", Secret: false, Default: "false", Description: "Whether to skip OCR processing for PDFs that already have OCR. Works with `pdf` and `whole_pdf` processing modes (`OCR_PROCESS_MODE`)."},
	{Name: "PDF_UPLOAD", Category: "PDF & hOCR", Secret: false, Default: "false", Description: "Whether to upload enhanced PDFs to paperless-ngx."},
	{Name: "PGID", Category: "Server & container", Secret: false, Default: "10001", Description: "Group ID to run the container as. See Running as a Non-Root User."},
	{Name: "PUID", Category: "Server & container", Secret: false, Default: "10001", Description: "User ID to run the container as. See Running as a Non-Root User."},
	{Name: "REMOVE_FROM_CONTENT", Category: "Processing & limits", Secret: false, Default: "", Description: "Comma-separated list of literal strings removed from document content before it is sent to the LLM for suggestions/analysis. Useful for stripping boilerplate (e.g. scanner watermarks) that confuses the model."},
	{Name: "REMOVE_FROM_CONTENT_REGEX", Category: "Processing & limits", Secret: false, Default: "", Description: "Semicolon-separated list of regular expressions removed from document content before it is sent to the LLM. Invalid patterns cause a startup error."},
	{Name: "SUGGESTION_JOB_TIMEOUT_SECONDS", Category: "Processing & limits", Secret: false, Default: "", Description: "Optional timeout for async manual suggestion jobs. Leave unset to disable; set a bounded value for slow local inference when jobs must not run forever."},
	{Name: "SUGGESTION_WORKERS", Category: "Processing & limits", Secret: false, Default: "1", Description: "Number of async manual suggestion workers. Keep this at `1` for slow or local LLM backends to avoid concurrent generation overload."},
	{Name: "TOKEN_LIMIT", Category: "Processing & limits", Secret: false, Default: "", Description: "Maximum tokens allowed for prompts/content. Set to `0` to disable limit. Useful for smaller LLMs."},
	{Name: "VISION_LLM_BACKOFF_MAX_WAIT", Category: "OCR", Secret: false, Default: "30s", Description: "Maximum wait time between retries for the Vision LLM (e.g., `30s`)."},
	{Name: "VISION_LLM_MAX_RETRIES", Category: "OCR", Secret: false, Default: "3", Description: "Maximum retry attempts for failed Vision LLM requests."},
	{Name: "VISION_LLM_MAX_TOKENS", Category: "OCR", Secret: false, Default: "", Description: "Maximum tokens for Vision LLM OCR output."},
	{Name: "VISION_LLM_MODEL", Category: "OCR", Secret: false, Default: "", Description: "Model name for LLM OCR (e.g. `minicpm-v`). Required if OCR_PROVIDER is `llm`."},
	{Name: "VISION_LLM_PROVIDER", Category: "OCR", Secret: false, Default: "", Description: "AI backend for LLM OCR (`openai`, `ollama`, `mistral`, or `anthropic`). Required if OCR_PROVIDER is `llm`."},
	{Name: "VISION_LLM_REQUESTS_PER_MINUTE", Category: "OCR", Secret: false, Default: "120", Description: "Maximum requests per minute for the Vision LLM. Useful for managing API costs or local LLM load."},
	{Name: "VISION_LLM_TEMPERATURE", Category: "OCR", Secret: false, Default: "", Description: "Sampling temperature for Vision OCR generation. Lower is more deterministic. Important: For OpenAI GPT-5 it must be explicitly set to `1.0`."},
}

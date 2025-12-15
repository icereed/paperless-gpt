# Tongyi (通义千问) provider

This project includes a simple Tongyi provider implementation. To use it, set the environment variables before starting the server:

```bash
export LLM_PROVIDER="tongyi"
export TONGYI_API_KEY="your_api_key"
export TONGYI_ENDPOINT="https://api.your-tongyi-endpoint"
export TONGYI_MODEL="model-name" # optional, falls back to LLM_MODEL
```

The client in `app_llm_tongyi.go` expects to POST JSON to `${TONGYI_ENDPOINT}/generate` with a body like `{"model":"<model>","prompt":"<text>"}` and expects a JSON response `{"result":"text"}`. Adjust the request and response parsing in `app_llm_tongyi.go` to match the real Tongyi API if different.

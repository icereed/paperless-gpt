# OpenAI-compatible providers

paperless-gpt does not need a dedicated code path for every AI vendor. Any
service that speaks the OpenAI chat-completions API works today with
`LLM_PROVIDER=openai` and `OPENAI_BASE_URL` pointed at it — no new release, no
patch, no fork.

If you came here from an issue asking for "support for X", this is almost
certainly the answer.

## The pattern

```yaml
environment:
  LLM_PROVIDER: "openai"
  OPENAI_BASE_URL: "https://api.example.com/v1" # the vendor's endpoint
  OPENAI_API_KEY: "<your key for that vendor>"
  LLM_MODEL: "<a model name that vendor accepts>"
```

Three things to get right:

1. **`OPENAI_BASE_URL` must point at the API root that serves
   `/chat/completions`**, not at the vendor's homepage or docs. For most
   services that means the URL ends in `/v1`.
2. **`LLM_MODEL` must be a name the vendor recognises**, which is often not the
   same string OpenAI uses. Check the vendor's model list.
3. **`OPENAI_API_KEY` holds the vendor's key.** The variable is named after the
   protocol, not the company — you are not sending anything to OpenAI.

The same applies to vision/OCR with `VISION_LLM_PROVIDER=openai` and
`VISION_LLM_MODEL`, provided the endpoint supports image inputs. Many
text-only gateways do not.

## Verified configurations

These have been reported working by users. Model names change often, so treat
them as starting points rather than guarantees.

### OpenRouter

```yaml
LLM_PROVIDER: "openai"
OPENAI_BASE_URL: "https://openrouter.ai/api/v1"
OPENAI_API_KEY: "sk-or-..."
LLM_MODEL: "anthropic/claude-sonnet-4-5"
```

### LM Studio

Enable the local server in LM Studio, then:

```yaml
LLM_PROVIDER: "openai"
OPENAI_BASE_URL: "http://host.docker.internal:1234/v1"
OPENAI_API_KEY: "lm-studio" # any non-empty value; LM Studio ignores it
LLM_MODEL: "<the model identifier shown in LM Studio>"
```

### vLLM

```yaml
LLM_PROVIDER: "openai"
OPENAI_BASE_URL: "http://vllm:8000/v1"
OPENAI_API_KEY: "not-used" # any non-empty value unless you configured a key
LLM_MODEL: "<the model you served with --model>"
```

### LiteLLM proxy

```yaml
LLM_PROVIDER: "openai"
OPENAI_BASE_URL: "http://litellm:4000/v1"
OPENAI_API_KEY: "<your LiteLLM virtual key>"
LLM_MODEL: "<the model alias configured in LiteLLM>"
```

### llama.cpp server

```yaml
LLM_PROVIDER: "openai"
OPENAI_BASE_URL: "http://llamacpp:8080/v1"
OPENAI_API_KEY: "not-used"
LLM_MODEL: "<any string; llama.cpp serves the model it was started with>"
```

### Azure OpenAI

Azure is the one case with its own switch, because the URL layout and auth
differ:

```yaml
LLM_PROVIDER: "openai"
OPENAI_API_TYPE: "azure"
OPENAI_BASE_URL: "https://your-resource.openai.azure.com"
OPENAI_API_KEY: "<your Azure key>"
LLM_MODEL: "<your deployment name>"
```

Note that `LLM_MODEL` is the **deployment** name, not the base model name.

### Other services

Anything else that documents an "OpenAI-compatible endpoint" — Groq, Together,
DeepInfra, Fireworks, Atlas Cloud, Chatanywhere, 1min.ai, Docker Model Runner,
Ollama's own `/v1` shim, and so on — follows the same three-line pattern. If a
service documents an OpenAI-compatible base URL, it works here.

## When to use the native Ollama provider instead

For Ollama, prefer `LLM_PROVIDER=ollama` with `OLLAMA_HOST` over its
OpenAI-compatible shim. The native path supports settings the shim does not
expose, including `OLLAMA_CONTEXT_LENGTH` and `OLLAMA_THINK`. Use the shim only
if you have a specific reason to.

## Troubleshooting

**`invalid character 'd' looking for beginning of value`**
The gateway answered with a server-sent-events stream while a plain JSON
response was expected — the `d` is the first byte of a `data:` frame. Some
gateways treat a missing `stream` field as "streaming on". See #1035.

**`404 page not found` or `404 model not found`**
Usually `OPENAI_BASE_URL` is missing the `/v1` suffix, or `LLM_MODEL` uses
OpenAI's spelling of a model the vendor names differently.

**`401 Unauthorized` from a local server**
Most local servers require the key to be non-empty even though they ignore its
value. Set it to any placeholder string.

**`API returned unexpected status code: 413`**
The request body exceeded the endpoint's limit. This is common in OCR mode with
high-resolution scans; lower `OCR_LIMIT_PAGES`, or raise the proxy's body-size
limit (`client_max_body_size` in nginx). See #791.

**`400 Unsupported value: 'temperature'`**
The model rejects the temperature being sent. Set `VISION_LLM_TEMPERATURE` (or
the equivalent for your path) to a value the model accepts — for OpenAI's GPT-5
family that is `1.0`. See #862 and #1003.

## Why there is no per-vendor setting

A dedicated `LLM_PROVIDER` value per vendor would add a code branch, an
environment variable, a required-key check and several provider lists to keep in
sync — for each vendor, permanently — while doing exactly what the two lines
above already do. Worse, hand-rolled branches tend to omit capabilities the
shared OpenAI path already has, such as the Azure handling and the base-URL
override.

A vendor genuinely needs its own provider only when the OpenAI-compatible shape
cannot express it: non-standard authentication headers, a different request
body, or endpoints that are not `/v1/chat/completions`. If you hit that, open an
issue with the failing request and response.

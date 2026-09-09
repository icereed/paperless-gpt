# Ollama generation settings

This document describes the Ollama metadata-generation settings for titles,
tags, correspondents, document types, created dates, custom fields, and ad-hoc
analysis. They do not configure Vision OCR and do not add a settings UI.

## Separate limits

These limits control different parts of a request and must not be treated as
interchangeable:

| Setting | Purpose |
| --- | --- |
| `TOKEN_LIMIT` | Existing global input truncation before a metadata prompt is sent. |
| `num_ctx` / `OLLAMA_CONTEXT_LENGTH` | Ollama runner context window. |
| `max_tokens` / `LLM_MAX_TOKENS` / `num_predict` | Maximum output-token budget for a metadata generation. |

A per-prompt `num_ctx` setting does not change `TOKEN_LIMIT`. The model context
must leave room for input, output, and, for models that produce it, thinking.
Values in examples are starting points, not performance benchmarks or quality
guarantees.

## Environment variables

`LLM_MAX_TOKENS` accepts a positive integer or `-1`. It maps to Ollama
`num_predict`; when it is unset, the model/base setting is preserved.

`OLLAMA_KEEP_ALIVE` accepts an Ollama duration such as `10m` or `1h`; `0`
requests unloading after the request and `-1` requests indefinite retention.
Keeping a model loaded can reserve substantial RAM or VRAM.

`OLLAMA_THINK` accepts `true`, `false`, `low`, `medium`, or `high`. Thinking
levels are model-specific. The pinned Ollama client does not expose `max` as an
application setting. GPT-OSS expects `low`, `medium`, or `high`; use a level
there instead of `false`. A model can reject or ignore an unsupported setting.
Thinking tokens can consume the output budget, and neither lower sampling nor a
fixed seed guarantees factual, deterministic output.

## Optional settings file

Set `OLLAMA_SETTINGS_FILE` to a JSON file available inside the container. Its
contents are captured during startup, so edit the file and restart the service
to change a running configuration. If the variable names a file, unknown keys,
invalid values, unreadable files, and invalid JSON fail startup; this avoids a
quiet fallback to different generation behavior. Omit a field to inherit its
lower-priority value; explicit JSON `null` is rejected.

The standard Compose example already mounts `./config` at `/app/config`. Place
the file there and enable it explicitly:

```yaml
environment:
  OLLAMA_SETTINGS_FILE: "/app/config/ollama-generation-settings.json"
volumes:
  - ./config:/app/config
```

The order of precedence is:

1. model/base setting;
2. `defaults` from the settings file;
3. explicitly valid environment variables;
4. matching per-prompt settings from the file;
5. explicit call options supplied by the application.

The optional settings fields are `temperature`, `max_tokens`, `num_ctx`,
`keep_alive`, `think`, `format`, `top_k`, `top_p`, `min_p`, `seed`,
`repeat_penalty`, `repeat_last_n`, `frequency_penalty`, `presence_penalty`, and
`stop`.

| Field | Accepted value | Effect |
| --- | --- | --- |
| `temperature` | Finite number ≥ 0 | Sampling temperature. |
| `max_tokens` | Positive integer or `-1` | Maps to `num_predict`, the output-token budget. |
| `num_ctx` | Positive integer | Ollama runner context window. |
| `keep_alive` | Duration, `0`, or `-1` | Retains, unloads, or indefinitely retains the loaded model. |
| `think` | Boolean, `low`, `medium`, or `high` | Requests thinking behavior or a model-specific thinking level. |
| `format` | `"json"` or a non-empty JSON Schema object | Requests JSON mode or a structured response where that prompt permits it. |
| `top_k` | Integer ≥ 0 | Limits the candidate set considered during sampling. |
| `top_p` | Finite number from 0 to 1 | Applies nucleus sampling. |
| `min_p` | Finite number from 0 to 1 | Filters tokens below a probability relative to the most likely token. |
| `seed` | Integer | Supplies a sampling seed for comparisons. |
| `repeat_penalty` | Finite number ≥ 0 | Adjusts repetition penalty. |
| `repeat_last_n` | Integer ≥ -1 | Sets the lookback window for repetition handling. |
| `frequency_penalty` | Finite number; negative values allowed | Adjusts token-frequency preference. |
| `presence_penalty` | Finite number; negative values allowed | Adjusts preference for already-seen tokens. |
| `stop` | Array of strings | Stops generation when a sequence is encountered. |

The table lists application validation, not model defaults. Ollama models can
support only a subset or interpret generation controls differently.

```json
{
  "defaults": {
    "temperature": 0,
    "max_tokens": 256,
    "keep_alive": "10m",
    "think": false
  },
  "prompts": {
    "title_prompt.tmpl": {
      "max_tokens": 48,
      "think": false
    },
    "custom_field_prompt.tmpl": {
      "max_tokens": 1024,
      "format": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "field": { "type": "string" },
            "value": {}
          },
          "required": ["field", "value"]
        }
      }
    }
  }
}
```

Supported prompt file names are `title_prompt.tmpl`, `tag_prompt.tmpl`,
`correspondent_prompt.tmpl`, `document_type_prompt.tmpl`,
`created_date_prompt.tmpl`, `custom_field_prompt.tmpl`, and
`adhoc-analysis_prompt.tmpl`.

## Structured output and sampling

No `format` is enabled by default. To avoid breaking existing plain-text and
CSV consumers, a settings-file `format` is accepted only for
`custom_field_prompt.tmpl` and `adhoc-analysis_prompt.tmpl`. Both accept
`"json"`; a custom-fields schema must have a root `"type": "array"` of
`{ "field", "value" }` objects because that is the existing parser contract.
An ad-hoc schema may have any root shape. The adapter confirms that a structured
response is valid JSON, but leaves full schema enforcement to Ollama rather
than reimplementing validation locally. Ad-hoc analysis remains opaque JSON
response text. The examples do not modify prompt templates.

Sampling settings are optional and keep model/base behavior when absent. `top_k`,
`top_p`, `min_p`, penalties, and seeds have model-specific support and may not
map one-to-one across Ollama models or APIs. For extraction, start by testing
temperature `0` with the chosen model and leave other sampling controls unset
until there is a measured need. Repetition penalties can suppress valid repeated
data, `stop` sequences can truncate JSON, and a seed is useful for comparisons
rather than a guarantee of identical or factual output.

Ollama documents the request fields and runtime options in its
[chat API](https://docs.ollama.com/api/chat) and its common generation
parameters in the [Modelfile reference](https://docs.ollama.com/modelfile).
Thinking behavior is model-dependent; see [Thinking](https://docs.ollama.com/capabilities/thinking).
JSON mode and schemas are described in [Structured outputs](https://docs.ollama.com/capabilities/structured-outputs).

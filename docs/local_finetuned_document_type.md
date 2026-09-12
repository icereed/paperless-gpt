# Local fine-tuned model for document type suggestions

This guide covers a small local model that was fine-tuned for one job: picking the document type with the prompt paperless-gpt ships. It runs in Ollama, so documents never leave your machine and there is no API bill for this field.

## Why

The shipped document type prompt was benchmarked on public data (200 documents from a corrected Tobacco3482 OCR dataset, 10 classes, 60 held-out rows, exact match). Results:

| Model | Score |
|---|---|
| gpt-5.4-mini, shipped prompt | 78.33 |
| gpt-5.4-mini, optimized prompt | 81.67 |
| Gemma 4 E4B, no fine-tune | 63.33 |
| Gemma 4 E4B, fine-tuned | **86.67** |

The fine-tuned 4B beats the cloud model running the same prompt by 8 points. Full method, data, notebooks, and caveats: [apprentice-benchmark, document type task](https://github.com/singhabhishekkk/apprentice-benchmark/tree/main/tasks/document-type-classification).

## Setup

Pull the model straight from Hugging Face into Ollama:

```bash
ollama run hf.co/singhabhishekkk/apprentice-gemma4-e4b-lora-document-types
```

Then point paperless-gpt at it:

```yaml
environment:
  LLM_PROVIDER: "ollama"
  LLM_MODEL: "hf.co/singhabhishekkk/apprentice-gemma4-e4b-lora-document-types"
  OLLAMA_HOST: "http://host.docker.internal:11434"
```

On native Linux Docker Engine, `host.docker.internal` needs an extra hosts mapping (Docker Desktop sets it up for you):

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

## Know the limits before you switch

- **This model is specialized for document type only.** paperless-gpt uses one text LLM for all suggestions (title, correspondent, tags, and type). If you switch `LLM_MODEL` to this one, the other fields have not been evaluated and may perform poorly. Use it if document type is the field you care about, or as a starting point for a per-field model option.
- It was trained on English tobacco-industry documents with 10 fixed classes. Your type list, languages, and OCR quality will differ. Test on your own documents first.
- 60 held-out rows is a small eval. The numbers above are honest but not a guarantee.

## License

The base model is Gemma 4 E4B (Apache-2.0 style Gemma terms, "Built with Gemma"). The adapter and GGUF are on Hugging Face under the same terms. The benchmark data is CC-BY-4.0.

## Support and resources

- [Benchmark and reproduction steps](https://github.com/singhabhishekkk/apprentice-benchmark/tree/main/tasks/document-type-classification)
- [Model on Hugging Face](https://huggingface.co/singhabhishekkk/apprentice-gemma4-e4b-lora-document-types)
- [paperless-gpt issues](https://github.com/icereed/paperless-gpt/issues)

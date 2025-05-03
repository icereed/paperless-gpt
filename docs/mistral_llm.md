# Mistral AI Integration in paperless-gpt (OCR and Vision LLM)

This guide covers how to use Mistral AI's capabilities in paperless-gpt, including both their Vision models and dedicated OCR service.

## Overview

Mistral AI provides two different approaches for OCR in paperless-gpt:

1. **Vision LLM**: Uses Mistral's Vision models for adaptable, customizable OCR with interactive capabilities
2. **Dedicated OCR Service**: Purpose-built OCR endpoint optimized for document processing

## Choosing Between OCR Methods

### 1. Dedicated OCR Provider (Recommended for most cases)
- More cost-effective
- Optimized for document processing
- Built-in document structure preservation
- Returns markdown-formatted text
- Best for standard OCR needs
- Limited to 50MB/1000 pages per document

### 2. Vision LLM Approach
- Highly customizable via prompts
- Can handle special formats or layouts
- More expensive
- Best for cases requiring:
  * Custom text interpretation
  * Special output formats
  * Complex document understanding

## Configuration

### Method 1: Dedicated OCR Provider

```yaml
environment:
  # OCR Configuration
  OCR_PROVIDER: "mistral_ocr"
  MISTRAL_API_KEY: "your_mistral_api_key"
  # Optional: specify model version
  MISTRAL_MODEL: "mistral-ocr-latest"
```

### Method 2: Vision LLM

```yaml
environment:
  # OCR Configuration
  OCR_PROVIDER: "llm"
  VISION_LLM_PROVIDER: "mistral"
  VISION_LLM_MODEL: "pixtral-large-latest"
  MISTRAL_API_KEY: "your_mistral_api_key"
```

## Size Limits and Constraints

### Dedicated OCR Provider
- Maximum file size: 50MB
- Maximum page count: 1,000 pages
- Supported formats: PDF, images (JPEG, PNG)

### Vision LLM
- Limits depend on the model used
- Generally handles single images or small documents better
- No explicit file size limit, but larger files may impact performance

## Best Practices

1. **Choosing the Right Method**
   - Use dedicated OCR provider for:
     * Batch processing
     * Large documents
     * Standard OCR needs
   - Use Vision LLM for:
     * Custom extraction requirements
     * Complex layouts needing interpretation
     * When you need to customize the OCR behavior

2. **Cost Optimization**
   - Dedicated OCR provider is more cost-effective for bulk processing
   - Vision LLM might be more expensive but offers more flexibility

3. **Performance Optimization**
   - For dedicated OCR:
     * Stay within size limits
     * Use markdown output for structured text
   - For Vision LLM:
     * Customize prompts for specific needs
     * Use appropriate context sizes

## Output Format

### Dedicated OCR Provider
- Returns markdown-formatted text
- Preserves document structure
- Maintains formatting like headers and lists
- Handles tables and columns

### Vision LLM
- Returns plain text by default
- Can be customized via prompts
- More flexible but requires more configuration

## Error Handling

Common issues and solutions:

1. **Authentication**
   ```
   Error: Invalid API key
   ```
   Solution: Verify your MISTRAL_API_KEY is correctly set

2. **Model Availability**
   ```
   Error: Model not available
   ```
   Solution: Check model name and your subscription level

## Support and Resources

- [Mistral AI Documentation](https://docs.mistral.ai/)
- [OCR API Reference](https://docs.mistral.ai/api#ocr)

For additional help:
- Check the [paperless-gpt issues](https://github.com/icereed/paperless-gpt/issues)
- Join our [Discord community](https://discord.gg/fJQppDH2J7)

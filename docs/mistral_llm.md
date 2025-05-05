# Mistral AI Integration in paperless-gpt (OCR)

This guide covers how to use Mistral AI's dedicated OCR service in paperless-gpt.

## Overview

Mistral AI provides a purpose-built OCR endpoint optimized for document processing. Unlike other providers, the Mistral LLM interface in the underlying library does not currently support image uploads, so only the dedicated OCR provider is available.

## OCR Capabilities

### Dedicated OCR Provider
- Cost-effective document processing
- Optimized for text extraction from documents
- Built-in document structure preservation
- Returns markdown-formatted text
- Best for standard OCR needs
- Limited to 50MB/1000 pages per document

## Configuration

```yaml
environment:
  # OCR Configuration
  OCR_PROVIDER: "mistral_ocr"
  MISTRAL_API_KEY: "your_mistral_api_key"
  # Optional: specify model version
  MISTRAL_MODEL: "mistral-ocr-latest"
```

## Size Limits and Constraints

- Maximum file size: 50MB
- Maximum page count: 1,000 pages
- Supported formats: PDF, images (JPEG, PNG)

## Best Practices

1. **Performance Optimization**
   - Stay within size limits
   - Use markdown output for structured text

2. **Cost Optimization**
   - The dedicated OCR provider is cost-effective for bulk processing

## Output Format

- Returns markdown-formatted text
- Preserves document structure
- Maintains formatting like headers and lists
- Handles tables and columns

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

# Workflows: different prompts for different documents

By default paperless-gpt has one auto-processing path: every document tagged with `AUTO_TAG` gets the same prompts and the same steps. That stops working once your archive holds very different things. An invoice needs a title with the invoice number and a custom field for the amount; a contract needs the contract number and term; a private letter only needs a sensible title.

A **workflow** is a second (third, fourth, …) processing path with its own trigger tag. Tag a document with the trigger and it is processed with that workflow's prompts and settings instead of the global ones.

## What a workflow defines

| Setting | What it does |
|---|---|
| **Trigger tag** (required) | Documents with this tag are processed by the workflow. The tag is removed once processing succeeds. |
| **Completion tag** (optional) | Added after successful processing, **instead of** `AUTO_TAG_COMPLETE`. Leave it empty to get `AUTO_TAG_COMPLETE` as usual. |
| **Generation steps** | Title, tags, correspondent, created date, document type, custom fields. Each one either inherits the global setting or is switched on or off for this workflow. |
| **Prompts** | Override any prompt (title, tags, correspondent, document type, created date, custom fields) for this workflow. Prompts you leave empty fall back to the global templates. |
| **OCR** (optional) | Run OCR before generating metadata, with its own page limit and, for the LLM OCR provider, its own OCR prompt. |

The global `AUTO_TAG` path keeps working unchanged, next to any number of workflows.

## Setting one up

1. Open **Workflows** in the paperless-gpt sidebar and click **New workflow**.
2. Give it a name and a trigger tag, for example `paperless-gpt-invoices`, and optionally a completion tag such as `paperless-gpt-invoices-done`.
3. Choose which steps run and write the prompts you want to override. A prompt left empty uses the global template, so you only write the ones that differ. The global templates on the Settings page are a good starting point to copy from.
4. Save. paperless-gpt creates both tags in paperless-ngx right away.

Then route documents to it the same way you route them to `AUTO_TAG` today: by hand, or with a paperless-ngx workflow, mail rule or matching rule that assigns the trigger tag. For example, a paperless-ngx workflow that adds `paperless-gpt-invoices` to everything from the mail account `invoices@…`.

## Examples

- **Invoices**: title prompt asks for "Vendor – Invoice number – Date"; custom fields for amount and due date; tags off, because invoices are tagged by a paperless-ngx rule.
- **Contracts**: OCR on, custom fields for contract number, start date and notice period, plus [document linking](document_linking.md) so amendments point at the contract.
- **Private mail**: title only, everything else off, so nothing about the correspondence ends up in tags or fields.

## Rules that keep documents from looping

paperless-gpt refuses a workflow whose tags would send documents back into processing:

- The trigger tag cannot be one of paperless-gpt's own tags (`AUTO_TAG`, `MANUAL_TAG`, `AUTO_OCR_TAG`, `FAIL_TAG`, `AUTO_TAG_COMPLETE`, `PDF_OCR_COMPLETE_TAG`) or another workflow's trigger.
- The completion tag cannot be the workflow's own trigger, another workflow's trigger, or a tag that starts processing (`AUTO_TAG`, `MANUAL_TAG`, `AUTO_OCR_TAG`).
- Workflows cannot be chained: one workflow's completion tag cannot be another one's trigger.

Tag names are compared case-insensitively, as in paperless-ngx.

If processing a document fails repeatedly (`AUTO_TAG_MAX_RETRIES`), the workflow's trigger tag is removed and `FAIL_TAG` is added, exactly as for `AUTO_TAG`.

## Where workflows are stored

Workflows are saved with the other settings made in the web UI (`config/settings.json`). They are also available over the API: `GET`/`POST /api/workflows` and `PUT`/`DELETE /api/workflows/{id}`.

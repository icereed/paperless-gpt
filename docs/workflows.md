# AI Workflows: different prompts for different documents

By default paperless-gpt has one auto-processing path: every document tagged with `AUTO_TAG` gets the same prompts and the same steps. That stops working once your archive holds very different things. An invoice needs a title with the invoice number and a custom field for the amount. A contract needs the contract number and its notice period. A private letter only needs a sensible title.

An **AI workflow** is another processing path with its own trigger tag. Tag a document with the trigger and paperless-gpt processes it with that workflow's prompts and steps instead of the global ones, then swaps the trigger for the workflow's completion tag.

```
invoices  ──▶  title (own prompt), custom fields  ──▶  invoices-done
contracts ──▶  OCR, title, custom fields          ──▶  contracts-done
paperless-gpt-auto (AUTO_TAG) ──▶  global prompts and settings  ──▶  AUTO_TAG_COMPLETE
```

The global `AUTO_TAG` path keeps working unchanged, next to any number of workflows.

## Setting one up

1. Open **AI Workflows** in the paperless-gpt sidebar and click **New workflow**.
2. Give it a name and a trigger tag, for example `invoices`. Pick an existing paperless-ngx tag from the suggestions or type a new one; new tags are created in paperless-ngx when you save.
3. Choose which steps run. **Default** follows the `AUTO_GENERATE_*` settings, and the editor shows what that currently means.
4. Change only the prompts that should differ. Empty prompts use the global ones, which the editor shows greyed out; **Customize** copies the global prompt in as a starting point.
5. Click **Test on a document** and pick a document. You see what the workflow would set: title, tags, correspondent, document type, date and custom fields, next to the current values. Nothing is written to paperless-ngx, and unsaved changes are included, so you can tune a prompt until the result is right.
6. Save.

Then route documents to it the same way you route them to `AUTO_TAG` today: by hand, or with a paperless-ngx workflow, mail rule or matching rule that assigns the trigger tag. For example, a paperless-ngx workflow that adds `invoices` to everything arriving at the mail account `invoices@…`.

Each workflow card shows how many documents are currently waiting for it.

## What a workflow defines

| Setting | What it does |
|---|---|
| **Trigger tag** (required) | Documents with this tag are processed by the workflow. The tag is removed once processing succeeds. |
| **Completion tag** | Added after successful processing, **instead of** `AUTO_TAG_COMPLETE`. Leave it empty to get `AUTO_TAG_COMPLETE`. |
| **Steps** | Title, tags, correspondent, document type, created date, custom fields. Each one is Default, On or Off. |
| **OCR** | Run OCR before generating metadata, with its own page limit and, for the LLM OCR provider, its own OCR prompt. |
| **Prompts** | Your own version of any prompt. Prompts you leave empty use the global ones from Settings. |

## Workflows are files

Each workflow is a folder next to your global prompts:

```
prompts/
  title_prompt.tmpl              global prompts
  ...
  workflows/
    invoices/
      workflow.json              name, tags, steps, OCR settings
      title_prompt.tmpl          only the prompts this workflow changes
      custom_field_prompt.tmpl
```

- They are kept by the `./prompts` volume you already mount for your prompts.
- Prompt files have the same names as the global ones, so you can copy a global prompt into a workflow folder and edit it.
- You can create or change workflows by editing these files directly, or keep them in git. paperless-gpt picks up changes within one poll cycle, without a restart, and creates missing tags in paperless-ngx. A workflow with a broken `workflow.json` is skipped with an error in the log; the others keep running.
- The folder name is the workflow's ID: lowercase letters, digits, `-` and `_`.

A minimal `workflow.json`:

```json
{
  "version": 1,
  "name": "Invoices",
  "trigger_tag": "invoices",
  "completion_tag": "invoices-done",
  "generate_tags": false
}
```

Steps you leave out use the default. Other optional fields: `generate_titles`, `generate_correspondents`, `generate_document_types`, `generate_created_date`, `generate_custom_fields`, `enable_ocr`, `ocr_limit_pages`.

If you used workflows from an earlier build, they were stored in `config/settings.json`. paperless-gpt moves them into `prompts/workflows/` on the first start and logs each one it moved.

## Rules that keep documents from looping

paperless-gpt refuses a workflow whose tags would send documents back into processing:

- The trigger tag cannot be one of paperless-gpt's own tags (`AUTO_TAG`, `MANUAL_TAG`, `AUTO_OCR_TAG`, `FAIL_TAG`, `AUTO_TAG_COMPLETE`, `PDF_OCR_COMPLETE_TAG`) or another workflow's trigger.
- The completion tag cannot be the workflow's own trigger, another workflow's trigger, or a tag that starts processing (`AUTO_TAG`, `MANUAL_TAG`, `AUTO_OCR_TAG`).
- Workflows cannot be chained: one workflow's completion tag cannot be another one's trigger.

Tag names are compared case-insensitively, as in paperless-ngx. The editor points these out while you type. The same rules apply to workflow files edited by hand: a workflow that breaks them is skipped with an error in the log, and when two workflows collide the one whose folder name sorts first is used.

A document that carries both a workflow trigger and `AUTO_TAG` is processed once, by the workflow, and both tags are removed.

If processing a document fails repeatedly (`AUTO_TAG_MAX_RETRIES`), the workflow's trigger tag is removed and `FAIL_TAG` is added, exactly as for `AUTO_TAG`.

## API

| Method and path | Purpose |
|---|---|
| `GET /api/workflows` | List workflows |
| `POST /api/workflows` | Create a workflow |
| `PUT /api/workflows/{id}` | Replace a workflow |
| `DELETE /api/workflows/{id}` | Delete a workflow and its folder |
| `GET /api/workflows/{id}/documents` | Documents waiting for the workflow |
| `POST /api/workflows/preview` | Run a workflow (`{"workflow": {...}, "document_id": 123}`) without changing anything |
| `GET /api/workflows/defaults` | What workflows inherit: global steps and prompts |

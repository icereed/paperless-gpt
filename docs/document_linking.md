# Automatically link related documents

A dunning letter refers to an invoice. A credit note refers to an invoice. A contract amendment refers to the contract, a delivery note to an order, a notice of objection to an official decision. The connection is always the same: a reference number printed on both documents.

paperless-gpt can read that reference and set a **Document Link** custom field in paperless-ngx that points at the other document. paperless-ngx shows the link on both sides, so the invoice lists its reminder and the reminder lists its invoice. Nobody has to search for the number and link the two by hand.

## What it looks like

A reminder arrives:

> **Zahlungserinnerung** — Rechnung Nr. **RE-2026-0417** vom 03.08.2026 ist noch offen.

paperless-gpt finds `RE-2026-0417`, looks up which document in your archive contains that number, and links the reminder to the original invoice. Open either one and you can jump to the other.

## Where this saves real work

Most office paperwork is a chain of documents that belong together. Linking them is what turns a pile of PDFs into a case file.

| Document that arrives | Links to | Typical reference |
|---|---|---|
| Payment reminder, dunning letter | the unpaid invoice | invoice number |
| Credit note, cancellation invoice | the original invoice | invoice number |
| Invoice | the order or purchase order | order / PO number |
| Delivery note | the order, the invoice | order or delivery number |
| Contract amendment, termination, renewal | the contract | contract number |
| Claim correspondence | the insurance policy, earlier claim letters | policy or claim number |
| Notice of objection, appeal | the official decision it contests | file number (Aktenzeichen) |
| Follow-up letter from an authority, court or lawyer | the earlier letters in the same case | file or case number |
| Collection agency letter | the original creditor's invoice or reminder | reference number |

A few setups where this changes how the archive is used:

- **Accounts payable.** Order, delivery note and invoice end up linked to each other, which is the starting point for checking that what was invoiced was ordered and delivered.
- **Contract management.** A contract carries its amendments, renewals and the termination letter, instead of those being scattered across years of scans.
- **Case files** for insurance claims, disputes, tax office correspondence or anything with an Aktenzeichen: every letter that cites the number is attached to the case automatically, regardless of which correspondent sent it.
- **Small offices and freelancers** who get reminders and collection letters: the reminder points straight at the invoice it is about, so checking whether it was already paid takes one click.

## Setup

1. **In paperless-ngx**, create a custom field of type **Document Link** (Settings → Custom Fields), for example named `Related documents`.
2. **In paperless-gpt**, open **Settings**, enable custom field generation and select that field. Choose the **Append** or **Update** write mode (see the note on **Replace** below).
3. Process documents as usual, through manual review or with the `paperless-gpt-auto` tag. You can combine the link field with any other custom fields you already extract.

There is no extra environment variable to set; `DOCUMENT_LINK_LLM_CHECK` (see below) is optional. The settings from step 2 are stored in `/app/config/settings.json`, so mount `./config:/app/config` as a volume; otherwise they are reset whenever the container is recreated, for example on every update.

## How it decides what to link

The model is never asked to guess paperless-ngx document IDs. The work is split in three:

1. **The LLM reads the document** and lists the identifiers it refers to: invoice, order, contract or case numbers.
2. **paperless-gpt looks each identifier up itself**, searching document text in paperless-ngx for an exact whole-word match. `R123` does not match `R1234`.
3. **The LLM checks the matches.** It sees the document and each match: title, date, correspondent, document type, the beginning of the text and the text around the identifier. Only the matches it confirms are linked.

The check is what tells a contract number apart from a number that is merely printed on many documents: your customer number on every letter from a company, a personnel or child benefit number filled into every salary form, a company register number in every letterhead. Counting matches can't make that distinction. In a small archive such a number may appear only twice; in a large one, the letters of a single file with an Aktenzeichen can be a dozen.

The lookup and the check are deliberately conservative, because a wrong link is worse than a missing one:

- References shorter than 4 characters are ignored.
- A reference has to look like an identifier: it needs a digit, and a number made of digits alone needs at least 5 of them. Names of laws or products, a year, or a 4-digit form number printed on every letter from a bank are not looked up.
- A reference that appears in **more than 10** other documents is treated as too generic and is not checked. The whole archive counts, not just the first search results. A reference that appears in more than 200 documents, even only as part of a longer number, is skipped without looking further.
- When the LLM is unsure, it is told to link nothing. If its answer can't be read, or the call fails, nothing is linked for that reference. It can only pick among the matches, never another document.
- A document is never linked to itself.
- If no reference resolves, the field is left out of the update entirely rather than being written with a guess.

The check costs one extra LLM call per reference that has matches. To skip it, set `DOCUMENT_LINK_LLM_CHECK=false`: then a reference is linked to all its matches if it appears in at most 3 other documents, and skipped otherwise.

The lookup uses paperless-ngx's database filter, not the full-text index, so it works the same on paperless-ngx 2.x and 3.x and does not depend on the search index being up to date.

## Adjusting the check

The check uses its own prompt, `document_link_prompt.tmpl`, which you can edit in the prompt editor like the others. What counts as "refers to" depends on the documents. The default links a document to the one the identifier belongs to, and to documents in the same matter. It does not link documents that only share a standing number, such as a customer or tax number.

Different kinds of documents may need different rules, so [AI workflows](workflows.md) can bring their own version of this prompt. In the workflow editor it is listed as **Document links** under the prompts, next to **Custom fields**. Examples:

- An **invoices** workflow that links a reminder only to the invoice with this invoice number, not to earlier reminders.
- An **authority letters** workflow that links every letter citing the same Aktenzeichen, including the decision and every reply.

The prompt is a Go template. It gets `{{.Reference}}`, `{{.Source}}` and `{{.Candidates}}`; see [the template variables](../README.md#template-variables) for their fields. The answer has to be a JSON array of the chosen ids, for example `[412]`, or `[]`.

## Good to know

- **The referenced document has to be in paperless-ngx first.** A link is made when the *referring* document is processed. If the reminder arrives before the invoice is scanned, there is nothing to link to yet. Reprocessing the reminder later picks it up.
- **The referenced document needs readable text.** Matching runs on the document content, so a scan without OCR text cannot be found. paperless-gpt's own OCR helps here.
- **Only documents the API token's user can see** are considered. Documents owned by other users without shared permissions won't be linked.
- **Avoid the Replace write mode for this field.** paperless-ngx mirrors each link onto the target document. With Replace, processing the target document later can overwrite that mirrored link. Append or Update leave it alone.
- **Custom prompts keep working.** The instructions for Document Link fields are generated into the field list itself, so a customized `custom_field_prompt.tmpl` gets them too, as long as it still includes `{{.CustomFieldsXML}}`.

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

There is no extra environment variable to set. The settings from step 2 are stored in `/app/config/settings.json`, so mount `./config:/app/config` as a volume; otherwise they are reset whenever the container is recreated, for example on every update.

## How it decides what to link

The model is never asked to guess paperless-ngx document IDs. The work is split in two:

1. **The LLM reads the document** and lists the identifiers it refers to: invoice, order, contract or case numbers.
2. **paperless-gpt looks each identifier up itself**, searching document text in paperless-ngx for an exact whole-word match. `R123` does not match `R1234`.

The lookup is deliberately conservative, because a wrong link is worse than a missing one:

- References shorter than 4 characters are ignored.
- A reference that appears in **more than 3** other documents is treated as too generic and skipped. This catches identifiers such as your own customer number, which shows up on every letter from the same company.
- A document is never linked to itself.
- If a reference matches nothing, it is dropped. If no reference resolves, the field is left out of the update entirely rather than being written with a guess.

The lookup uses paperless-ngx's database filter, not the full-text index, so it works the same on paperless-ngx 2.x and 3.x and does not depend on the search index being up to date.

## Good to know

- **The referenced document has to be in paperless-ngx first.** A link is made when the *referring* document is processed. If the reminder arrives before the invoice is scanned, there is nothing to link to yet. Reprocessing the reminder later picks it up.
- **The referenced document needs readable text.** Matching runs on the document content, so a scan without OCR text cannot be found. paperless-gpt's own OCR helps here.
- **Only documents the API token's user can see** are considered. Documents owned by other users without shared permissions won't be linked.
- **Avoid the Replace write mode for this field.** paperless-ngx mirrors each link onto the target document. With Replace, processing the target document later can overwrite that mirrored link. Append or Update leave it alone.
- **Custom prompts keep working.** The instructions for Document Link fields are generated into the field list itself, so a customized `custom_field_prompt.tmpl` gets them too, as long as it still includes `{{.CustomFieldsXML}}`.

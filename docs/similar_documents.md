# Similar documents

paperless-gpt can show its own suggestion model a few documents that are already like the one it is reading. The model then sees the tags, correspondent, and document type those documents use, spelled the way they exist in paperless-ngx. That is separate from [document links](document_linking.md), which match a printed reference number. This index does not create links.

paperless-ngx's own AI is not used, and its index is not read.

## What you configure

Set an embedding model. A chat model does not produce embeddings.

| Variable | Description | Default |
| --- | --- | --- |
| `EMBEDDING_PROVIDER` | `openai` or `ollama`. Empty leaves the feature off. `openai` also covers any OpenAI-compatible embeddings endpoint via `OPENAI_BASE_URL`. | |
| `EMBEDDING_MODEL` | The embedding model, for example `nomic-embed-text` or `text-embedding-3-small`. | |
| `EMBEDDING_SIMILAR_LIMIT` | How many similar documents to include in a prompt. Values above 20 are capped. | `5` |

`openai` reuses `OPENAI_API_KEY` and `OPENAI_BASE_URL`. `ollama` reuses `OLLAMA_HOST`.

## Building the index

Open **Settings** and choose **Build index**. paperless-gpt asks paperless-ngx for each document's id and modification time, then downloads and embeds only documents that are new or changed. Documents with no text are skipped. **Rebuild index** embeds every document again.

The page shows how many documents are in the local index and how many paperless-ngx reports. A build keeps running if you leave the page. Cancel stops between documents and keeps what was already written.

Until at least one other document is indexed, suggestion prompts are unchanged. Documents paperless-gpt processes later are added to the index as they are suggested, so the archive stays current between builds.

## Tags to leave out

Settings has a field, **Tags to leave out of suggestions**, for structural tags such as an inbox or an AI-review queue. It searches the tags already in paperless-ngx, the same way suggestion review does, and a name that is not in that list can still be added. Those tags are omitted from the similar-document block, including documents already indexed, and they are dropped when the model suggests them. Applying a suggestion removes them from the document, including a tag that was already on it, so the list can mark work that is still in progress. Saving an empty field leaves every tag in play. The list is stored in `config/settings.json` and takes effect on the next suggestion, without a restart or a rebuild.

The index is one row per document in paperless-gpt's SQLite database (`db/modification_history.db`). Mount a volume for `db/` if you run in Docker, or the index is lost when the container is recreated.

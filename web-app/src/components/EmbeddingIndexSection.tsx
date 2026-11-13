import { ArrowPathIcon } from "@heroicons/react/24/outline";
import axios from "axios";
import React, { useEffect, useState } from "react";
import { ReactTags } from "react-tag-autocomplete";
import { TagOption } from "../DocumentProcessor";
import Button from "./ui/Button";
import ConfirmDialog from "./ui/ConfirmDialog";

interface EmbeddingStatus {
  enabled: boolean;
  model?: string;
  state: "idle" | "running";
  mode?: string;
  available: number;
  indexed: number;
  processed: number;
  skipped: number;
  failed: number;
  unchanged: number;
  current_document_id: number;
  error?: string;
}

const emptyStatus: EmbeddingStatus = {
  enabled: false,
  state: "idle",
  available: 0,
  indexed: 0,
  processed: 0,
  skipped: 0,
  failed: 0,
  unchanged: 0,
  current_document_id: 0,
};

function visited(status: EmbeddingStatus): number {
  return status.processed + status.skipped + status.failed + status.unchanged;
}

/**
 * Build and rebuild the local embedding index from paperless-ngx.
 * The embedding model itself is chosen with environment variables.
 */
const EmbeddingIndexSection: React.FC = () => {
  const [status, setStatus] = useState<EmbeddingStatus>(emptyStatus);
  const [loadError, setLoadError] = useState("");
  const [confirmRebuild, setConfirmRebuild] = useState(false);
  const [busy, setBusy] = useState(false);
  const [ignoredTags, setIgnoredTags] = useState<string[]>([]);
  const [availableTags, setAvailableTags] = useState<TagOption[]>([]);
  const [savingTags, setSavingTags] = useState(false);
  const [tagsMessage, setTagsMessage] = useState<string | null>(null);
  const [tagsError, setTagsError] = useState<string | null>(null);

  const applyStatus = (next: EmbeddingStatus) => {
    setStatus(next);
    setLoadError("");
  };

  useEffect(() => {
    let cancelled = false;
    axios
      .get<EmbeddingStatus>("./api/embeddings")
      .then((response) => {
        if (!cancelled) applyStatus(response.data);
      })
      .catch((error: unknown) => {
        console.error("Failed to load embedding index status:", error);
        if (!cancelled) setLoadError("Could not load the embedding index status.");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    axios
      .get("./api/settings")
      .then((response) => {
        if (cancelled) return;
        const tags = response.data?.settings?.ignored_tags;
        setIgnoredTags(Array.isArray(tags) ? tags.filter((tag) => typeof tag === "string") : []);
      })
      .catch((error: unknown) => {
        console.error("Failed to load ignored tags:", error);
        if (!cancelled) setTagsError("Could not load the tags to leave out.");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    axios
      .get<Record<string, number>>("./api/tags")
      .then((response) => {
        if (cancelled) return;
        setAvailableTags(
          Object.keys(response.data || {}).map((tag) => ({ id: tag, name: tag }))
        );
      })
      .catch((error: unknown) => {
        console.error("Failed to load tags:", error);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (status.state !== "running") return;
    const timer = window.setInterval(() => {
      axios
        .get<EmbeddingStatus>("./api/embeddings")
        .then((response) => applyStatus(response.data))
        .catch((error: unknown) => {
          console.error("Failed to load embedding index status:", error);
        });
    }, 2000);
    return () => window.clearInterval(timer);
  }, [status.state]);

  const start = async (mode: "build" | "rebuild") => {
    setBusy(true);
    setLoadError("");
    try {
      const response = await axios.post<EmbeddingStatus>(`./api/embeddings/${mode}`);
      setStatus(response.data);
    } catch (error) {
      if (axios.isAxiosError(error)) {
        const message = error.response?.data?.error;
        setLoadError(typeof message === "string" ? message : "Could not start the embedding index.");
      } else {
        setLoadError("Could not start the embedding index.");
      }
    } finally {
      setBusy(false);
      setConfirmRebuild(false);
    }
  };

  const saveIgnoredTags = () => {
    setSavingTags(true);
    setTagsError(null);
    setTagsMessage(null);
    axios
      .get("./api/settings")
      .then((response) => {
        const current = response.data?.settings ?? {};
        return axios.post("./api/settings", { ...current, ignored_tags: ignoredTags });
      })
      .then(() => {
        setTagsMessage(ignoredTags.length === 0 ? "No tags are left out." : "Saved.");
      })
      .catch((error: unknown) => {
        console.error("Failed to save ignored tags:", error);
        setTagsError("Could not save the tags to leave out.");
      })
      .finally(() => setSavingTags(false));
  };

  const cancel = async () => {
    setBusy(true);
    try {
      const response = await axios.post<EmbeddingStatus>("./api/embeddings/cancel");
      setStatus(response.data);
    } catch (error) {
      console.error("Failed to cancel embedding index:", error);
      setLoadError("Could not cancel the embedding index.");
    } finally {
      setBusy(false);
    }
  };

  const running = status.state === "running";
  const seen = visited(status);
  const total = Math.max(status.available, 1);
  const fraction = running ? Math.min(seen / total, 1) : 0;
  const indexedLabel = `${status.indexed.toLocaleString()} of ${status.available.toLocaleString()} documents indexed`;

  return (
    <section
      aria-labelledby="embeddings-heading"
      className="rounded-lg border border-line bg-surface p-6"
    >
      <h2 id="embeddings-heading" className="text-lg font-semibold">
        Similar documents
      </h2>
      <p className="mt-2 max-w-prose text-sm text-muted">
        Suggestions can prefer the tags, correspondents, and document types
        already used on similar documents. Build the index from paperless-ngx
        after setting <code className="text-ink">EMBEDDING_PROVIDER</code> and{" "}
        <code className="text-ink">EMBEDDING_MODEL</code>.
      </p>

      <div className="mt-4 max-w-prose">
        <p className="text-sm font-medium">Tags to leave out of suggestions</p>
        <p className="mt-1 text-sm text-muted">
          Inbox and review tags are left out of similar documents and are not suggested. Applying a
          suggestion removes them from the document, so they can mark work that is still in progress.
          Search the tags in paperless-ngx, or type a name that is not listed.
        </p>
        <div className="mt-2 flex flex-col gap-2 sm:flex-row sm:items-start">
          <div className="min-w-0 flex-1">
            <ReactTags
              selected={ignoredTags.map((tag, index) => ({
                id: index.toString(),
                name: tag,
                label: tag,
                value: index.toString(),
              }))}
              suggestions={availableTags
                .filter(
                  (tag) =>
                    !ignoredTags.some((selected) => selected.toLowerCase() === tag.name.toLowerCase())
                )
                .sort((a, b) => a.name.localeCompare(b.name))
                .map((tag) => ({
                  id: tag.id,
                  name: tag.name,
                  label: tag.name,
                  value: tag.id,
                }))}
              onAdd={(tag) => {
                const name = String(tag.label).trim();
                if (!name) return;
                setIgnoredTags((current) =>
                  current.some((item) => item.toLowerCase() === name.toLowerCase())
                    ? current
                    : [...current, name]
                );
                setTagsMessage(null);
              }}
              onDelete={(index) => {
                setIgnoredTags((current) => current.filter((_, item) => item !== index));
                setTagsMessage(null);
              }}
              allowNew
              placeholderText="Add a tag"
              labelText="Tags to leave out of suggestions"
              isDisabled={savingTags}
            />
          </div>
          <Button variant="secondary" loading={savingTags} onClick={saveIgnoredTags}>
            Save
          </Button>
        </div>
        {tagsMessage && (
          <p className="mt-2 text-sm text-muted" role="status">
            {tagsMessage}
          </p>
        )}
        {tagsError && (
          <p className="mt-2 text-sm text-neg" role="alert">
            {tagsError}
          </p>
        )}
      </div>

      {!status.enabled && (
        <p className="mt-4 text-sm text-muted">
          No embedding model is configured, so these actions are unavailable.
        </p>
      )}

      {status.enabled && (
        <p className="mt-4 text-sm font-medium" role="status">
          {indexedLabel}
          {status.model ? ` · ${status.model}` : ""}
        </p>
      )}

      {running && (
        <div className="mt-4">
          <p className="text-sm text-muted">
            {status.mode === "rebuild" ? "Rebuilding" : "Building"} — {seen.toLocaleString()} of{" "}
            {status.available.toLocaleString()} checked
            {status.current_document_id > 0 ? `, document ${status.current_document_id}` : ""}
          </p>
          <div
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={status.available}
            aria-valuenow={seen}
            aria-label="Documents checked"
            className="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-2"
          >
            <div
              className="h-full origin-left rounded-full bg-primary transition-transform duration-300 ease-out-quart"
              style={{ transform: `scaleX(${fraction})` }}
            />
          </div>
          <p className="mt-2 text-xs text-faint">
            {status.processed.toLocaleString()} embedded, {status.unchanged.toLocaleString()} unchanged,{" "}
            {status.skipped.toLocaleString()} skipped, {status.failed.toLocaleString()} failed.
            The index keeps running if you leave this page.
          </p>
        </div>
      )}

      {!running && status.enabled && (status.processed > 0 || status.failed > 0 || status.skipped > 0) && (
        <p className="mt-2 text-xs text-faint">
          Last {status.mode === "rebuild" ? "rebuild" : "build"}: {status.processed.toLocaleString()} embedded,{" "}
          {status.unchanged.toLocaleString()} unchanged, {status.skipped.toLocaleString()} skipped,{" "}
          {status.failed.toLocaleString()} failed.
        </p>
      )}

      {(loadError || status.error) && (
        <p className="mt-3 text-sm text-neg" role="alert">
          {loadError || status.error}
        </p>
      )}

      <div className="mt-4 flex flex-wrap gap-2">
        <Button
          variant="primary"
          disabled={!status.enabled || running || busy}
          loading={busy && !running}
          onClick={() => void start("build")}
        >
          <ArrowPathIcon className="h-4 w-4" aria-hidden="true" />
          Build index
        </Button>
        <Button
          variant="secondary"
          disabled={!status.enabled || running || busy}
          onClick={() => setConfirmRebuild(true)}
        >
          Rebuild index
        </Button>
        {running && (
          <Button variant="danger" disabled={busy} onClick={() => void cancel()}>
            Cancel
          </Button>
        )}
      </div>

      <ConfirmDialog
        open={confirmRebuild}
        title="Rebuild the embedding index?"
        body="Rebuild embeds every document again. That calls the embedding model once for each document that has text."
        confirmLabel="Rebuild"
        onConfirm={() => void start("rebuild")}
        onCancel={() => setConfirmRebuild(false)}
      />
    </section>
  );
};

export default EmbeddingIndexSection;

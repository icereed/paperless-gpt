import { ArrowPathIcon } from "@heroicons/react/24/outline";
import axios from "axios";
import React, { useCallback, useEffect, useRef, useState } from "react";
import Button from "./components/ui/Button";
import Toast, { ToastData } from "./components/ui/Toast";
import DocumentsToProcess from "./components/DocumentsToProcess";
import GenerationOptions, {
  GenerationFlags,
} from "./components/GenerationOptions";
import JobProgress from "./components/JobProgress";
import NoDocuments from "./components/NoDocuments";
import SuggestionsReview from "./components/SuggestionsReview";

export interface Document {
  id: number;
  title: string;
  content: string;
  tags: string[];
  correspondent: string;
  created_date?: string;
  document_type_name?: string;
}

export interface GenerateSuggestionsRequest {
  documents: Document[];
  generate_titles?: boolean;
  generate_tags?: boolean;
  generate_correspondents?: boolean;
  generate_document_types?: boolean;
  generate_created_date?: boolean;
  generate_custom_fields?: boolean;
  selected_custom_field_ids?: number[];
  custom_field_write_mode?: string;
}

export interface CustomFieldSuggestion {
  id: number;
  value: unknown;
  name: string;
  isSelected: boolean;
}

export interface DocumentSuggestion {
  id: number;
  original_document: Document;
  suggested_title?: string;
  suggested_tags?: string[];
  suggested_content?: string;
  suggested_correspondent?: string;
  suggested_document_type?: string;
  suggested_created_date?: string;
  suggested_custom_fields?: CustomFieldSuggestion[];
}

export interface TagOption {
  id: string;
  name: string;
}

export interface SuggestionJobFailedDocument {
  document_id: number;
  document_title: string;
  error: string;
}

export interface SuggestionJobStatus {
  job_id: string;
  status: "pending" | "in_progress" | "completed" | "failed" | "cancelled";
  documents_done: number;
  total_documents: number;
  current_document_id: number;
  result?: DocumentSuggestion[];
  error?: string;
  failed_documents?: SuggestionJobFailedDocument[];
}

interface CustomField {
  id: number;
  name: string;
  data_type: string;
}

const ACTIVE_JOB_KEY = "pgpt-active-suggestion-job";
const POLL_INTERVAL_MS = 1500;

const DocumentProcessor: React.FC = () => {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [suggestions, setSuggestions] = useState<DocumentSuggestion[] | null>(
    null
  );
  const [availableTags, setAvailableTags] = useState<TagOption[]>([]);
  const [allCustomFields, setAllCustomFields] = useState<CustomField[]>([]);
  const [loading, setLoading] = useState(true);
  const [filterTag, setFilterTag] = useState<string | null>(null);
  const [flags, setFlags] = useState<GenerationFlags>({
    titles: true,
    tags: true,
    correspondents: true,
    documentTypes: true,
    createdDate: true,
    customFields: true,
  });
  const [job, setJob] = useState<SuggestionJobStatus | null>(null);
  const [failedDocuments, setFailedDocuments] = useState<
    SuggestionJobFailedDocument[]
  >([]);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastData | null>(null);
  const pollingDocumentsRef = useRef(false);
  const consumedJobsRef = useRef<Set<string>>(new Set());

  const jobRunning =
    job !== null && (job.status === "pending" || job.status === "in_progress");
  const phase: "select" | "generating" | "review" = jobRunning
    ? "generating"
    : suggestions
      ? "review"
      : "select";

  const fetchInitialData = useCallback(async () => {
    try {
      const [filterTagRes, documentsRes, tagsRes, customFieldsRes] =
        await Promise.all([
          axios.get<{ tag: string }>("./api/filter-tag"),
          axios.get<Document[]>("./api/documents"),
          axios.get<Record<string, number>>("./api/tags"),
          axios.get<CustomField[]>("./api/custom_fields"),
        ]);

      setFilterTag(filterTagRes.data.tag);
      setAllCustomFields(customFieldsRes.data || []);
      setDocuments(documentsRes.data);
      setSelectedIds(new Set(documentsRes.data.map((doc) => doc.id)));
      const tags = Object.keys(tagsRes.data).map((tag) => ({
        id: tag,
        name: tag,
      }));
      setAvailableTags(tags);
    } catch (err) {
      console.error("Error fetching initial data:", err);
      setError(
        "Could not reach the paperless-gpt backend. Check that the service is running, then retry."
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchInitialData();
  }, [fetchInitialData]);

  // Attach custom-field names to a job result and hand it to the review phase.
  const consumeJobResult = useCallback(
    (finishedJob: SuggestionJobStatus) => {
      if (consumedJobsRef.current.has(finishedJob.job_id)) return;
      consumedJobsRef.current.add(finishedJob.job_id);

      const customFieldMap = new Map(
        (allCustomFields || []).map((cf) => [cf.id, cf.name])
      );
      const processed = (finishedJob.result || []).map((suggestion) => ({
        ...suggestion,
        suggested_custom_fields: suggestion.suggested_custom_fields?.map(
          (cf) => ({
            ...cf,
            name: customFieldMap.get(cf.id) || "Unknown Field",
            isSelected: true,
          })
        ),
      }));

      setFailedDocuments(finishedJob.failed_documents || []);
      setSuggestions((prev) => (prev ? [...prev, ...processed] : processed));
    },
    [allCustomFields]
  );

  // Resume a job after a reload: the job id survives in localStorage and the
  // full result (including original documents) lives on the server. Wait for
  // the initial data (custom fields) before consuming a completed result —
  // otherwise every custom field name resolves to "Unknown Field" and the job
  // is marked consumed, so the names never recover.
  useEffect(() => {
    if (loading) return;
    const storedJobId = localStorage.getItem(ACTIVE_JOB_KEY);
    if (!storedJobId) return;
    (async () => {
      try {
        const { data } = await axios.get<SuggestionJobStatus>(
          `./api/jobs/suggestions/${storedJobId}`
        );
        if (data.status === "pending" || data.status === "in_progress") {
          setJob(data);
        } else if (data.status === "completed") {
          consumeJobResult(data);
        } else {
          localStorage.removeItem(ACTIVE_JOB_KEY);
        }
      } catch {
        localStorage.removeItem(ACTIVE_JOB_KEY);
      }
    })();
  }, [loading, consumeJobResult]);

  // Poll the active job until it reaches a terminal state.
  useEffect(() => {
    if (!jobRunning || !job) return;
    const interval = setInterval(async () => {
      try {
        const { data } = await axios.get<SuggestionJobStatus>(
          `./api/jobs/suggestions/${job.job_id}`
        );
        setJob(data);
        if (data.status === "completed") {
          consumeJobResult(data);
          if ((data.failed_documents || []).length === 0) {
            localStorage.setItem(ACTIVE_JOB_KEY, data.job_id);
          }
        } else if (data.status === "failed") {
          setError(data.error || "Suggestion generation failed.");
          setFailedDocuments(data.failed_documents || []);
          localStorage.removeItem(ACTIVE_JOB_KEY);
        } else if (data.status === "cancelled") {
          setToast({
            kind: "info",
            message: "Generation cancelled. Nothing was changed.",
          });
          localStorage.removeItem(ACTIVE_JOB_KEY);
        }
      } catch (err) {
        console.error("Error polling suggestion job:", err);
        setError(
          "Lost track of the generation job — the backend may have restarted. Start a new generation to continue."
        );
        localStorage.removeItem(ACTIVE_JOB_KEY);
        setJob(null);
      }
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [job, jobRunning, consumeJobResult]);

  const startGeneration = async (docsToProcess: Document[]) => {
    setError(null);
    setFailedDocuments([]);
    try {
      const payload: GenerateSuggestionsRequest = {
        documents: docsToProcess,
        generate_titles: flags.titles,
        generate_tags: flags.tags,
        generate_correspondents: flags.correspondents,
        generate_document_types: flags.documentTypes,
        generate_created_date: flags.createdDate,
        generate_custom_fields: flags.customFields,
      };
      const { data } = await axios.post<{ job_id: string }>(
        "./api/jobs/suggestions",
        payload
      );
      localStorage.setItem(ACTIVE_JOB_KEY, data.job_id);
      setJob({
        job_id: data.job_id,
        status: "pending",
        documents_done: 0,
        total_documents: docsToProcess.length,
        current_document_id: 0,
      });
    } catch (err) {
      console.error("Error starting suggestion job:", err);
      setError(
        "Could not start generating suggestions. Check the backend connection, then retry."
      );
    }
  };

  const handleGenerateClick = () => {
    const docsToProcess = documents.filter((doc) => selectedIds.has(doc.id));
    if (docsToProcess.length === 0) return;
    startGeneration(docsToProcess);
  };

  const handleRetryFailed = () => {
    const failedIds = new Set(failedDocuments.map((f) => f.document_id));
    const docsToRetry = documents.filter((doc) => failedIds.has(doc.id));
    if (docsToRetry.length === 0) {
      setFailedDocuments([]);
      return;
    }
    startGeneration(docsToRetry);
  };

  const handleCancelJob = async () => {
    if (!job) return;
    try {
      await axios.post(`./api/jobs/suggestions/${job.job_id}/stop`);
    } catch (err) {
      console.error("Error stopping suggestion job:", err);
    }
  };

  const handleToggleDocument = (documentId: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(documentId)) {
        next.delete(documentId);
      } else {
        next.add(documentId);
      }
      return next;
    });
  };

  const handleToggleAll = () => {
    setSelectedIds((prev) =>
      prev.size === documents.length
        ? new Set()
        : new Set(documents.map((doc) => doc.id))
    );
  };

  const reloadDocuments = async () => {
    setLoading(true);
    setError(null);
    try {
      const { data } = await axios.get<Document[]>("./api/documents");
      setDocuments(data);
      setSelectedIds(new Set(data.map((doc) => doc.id)));
    } catch (err) {
      console.error("Error reloading documents:", err);
      setError("Could not reload documents. Check the backend connection.");
    } finally {
      setLoading(false);
    }
  };

  // While the queue is empty, watch for newly tagged documents.
  useEffect(() => {
    if (documents.length === 0 && phase === "select") {
      const interval = setInterval(async () => {
        if (pollingDocumentsRef.current) return;
        pollingDocumentsRef.current = true;
        try {
          const { data } = await axios.get<Document[]>("./api/documents");
          if (data.length > 0) {
            setDocuments(data);
            setSelectedIds(new Set(data.map((doc) => doc.id)));
            setError(null);
          }
        } catch (err) {
          console.error("Error polling documents:", err);
        } finally {
          pollingDocumentsRef.current = false;
        }
      }, 5000);
      return () => clearInterval(interval);
    }
  }, [documents, phase]);

  const finishReview = (appliedCount: number, fieldChanges: number) => {
    localStorage.removeItem(ACTIVE_JOB_KEY);
    setSuggestions(null);
    setJob(null);
    setFailedDocuments([]);
    setToast({
      kind: "success",
      message:
        appliedCount === 0
          ? "Review closed. No documents were changed."
          : `Applied ${fieldChanges} field ${fieldChanges === 1 ? "change" : "changes"} to ${appliedCount} ${appliedCount === 1 ? "document" : "documents"}.`,
      action:
        appliedCount > 0
          ? { label: "Review in History", to: "/history" }
          : undefined,
    });
    reloadDocuments();
  };

  const discardReview = () => {
    localStorage.removeItem(ACTIVE_JOB_KEY);
    setSuggestions(null);
    setJob(null);
    setFailedDocuments([]);
  };

  if (loading && documents.length === 0 && phase === "select") {
    return (
      <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6" aria-busy="true">
        <div className="h-7 w-72 animate-pulse rounded bg-surface-2" />
        <div className="mt-2 h-4 w-96 animate-pulse rounded bg-surface-2" />
        <div className="mt-8 grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="h-40 animate-pulse rounded-lg border border-line bg-surface"
            />
          ))}
        </div>
        <span className="sr-only">Loading documents…</span>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6">
      {error && (
        <div
          role="alert"
          className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-neg bg-neg-tint px-4 py-3 text-sm text-neg"
        >
          <span>{error}</span>
          <div className="flex gap-2">
            {failedDocuments.length > 0 && (
              <Button size="sm" variant="secondary" onClick={handleRetryFailed}>
                Retry failed documents
              </Button>
            )}
            <Button size="sm" variant="secondary" onClick={() => setError(null)}>
              Dismiss
            </Button>
          </div>
        </div>
      )}

      {phase === "review" && suggestions ? (
        <SuggestionsReview
          suggestions={suggestions}
          availableTags={availableTags}
          filterTag={filterTag}
          failedDocuments={failedDocuments}
          onRetryFailed={handleRetryFailed}
          onFinished={finishReview}
          onDiscard={discardReview}
        />
      ) : documents.length === 0 && phase === "select" ? (
        <NoDocuments
          filterTag={filterTag}
          onReload={reloadDocuments}
          reloading={loading}
        />
      ) : (
        <section aria-label="Documents to process">
          <header className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h1 className="text-xl font-semibold">
                {documents.length}{" "}
                {documents.length === 1 ? "document" : "documents"} waiting for
                review
              </h1>
              <p className="mt-1 text-sm text-muted">
                Tagged with{" "}
                {filterTag ? (
                  <span className="rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium text-ink">
                    {filterTag}
                  </span>
                ) : (
                  "the filter tag"
                )}{" "}
                in paperless-ngx. Generate suggestions, review, then apply.
              </p>
            </div>
            <Button
              variant="secondary"
              onClick={reloadDocuments}
              disabled={phase === "generating" || loading}
            >
              <ArrowPathIcon className="h-4 w-4" aria-hidden="true" />
              Refresh
            </Button>
          </header>

          {phase === "generating" && job ? (
            <JobProgress
              job={job}
              documents={documents}
              onCancel={handleCancelJob}
            />
          ) : (
            <GenerationOptions
              flags={flags}
              onChange={setFlags}
              selectedCount={selectedIds.size}
              onGenerate={handleGenerateClick}
            />
          )}

          <div className="mt-6">
            <DocumentsToProcess
              documents={documents}
              selectedDocuments={Array.from(selectedIds)}
              onSelectDocument={
                phase === "generating" ? undefined : handleToggleDocument
              }
              onToggleAll={phase === "generating" ? undefined : handleToggleAll}
            />
          </div>
        </section>
      )}

      {toast && <Toast toast={toast} onDismiss={() => setToast(null)} />}
    </div>
  );
};

export default DocumentProcessor;

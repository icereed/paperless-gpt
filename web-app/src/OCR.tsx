import { StopIcon } from "@heroicons/react/24/outline";
import axios from "axios";
import classNames from "classnames";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { Document } from "./DocumentProcessor";
import ActivityTab from "./components/ocr/ActivityTab";
import DocumentPicker from "./components/ocr/DocumentPicker";
import RunOptionsPanel from "./components/ocr/RunOptionsPanel";
import RunResults from "./components/ocr/RunResults";
import {
  OCRConfig,
  OCRJobStatus,
  OCRRun,
  OCRRunOptions,
  fetchOCRConfig,
  fetchOCRJob,
  fetchOCRRuns,
  stopOCRJob,
  submitOCRJob,
} from "./components/ocr/api";
import Button from "./components/ui/Button";
import Toast, { ToastData } from "./components/ui/Toast";

interface RerunState {
  rerun?: {
    documentId: number;
    documentTitle: string;
    options: OCRRunOptions;
    promptOverride: string | null;
  };
}

const POLL_INTERVAL_MS = 1500;

/**
 * The OCR page: a Playground to run, inspect, tune, and apply LLM-based OCR
 * on any document — and an Activity log that makes hands-off Auto-OCR
 * trustworthy. The Playground is where users build the settings they later
 * promote to auto-mode defaults.
 */
const OCR: React.FC = () => {
  const [searchParams] = useSearchParams();
  const tab: "playground" | "activity" =
    searchParams.get("tab") === "activity" ? "activity" : "playground";
  const [config, setConfig] = useState<OCRConfig | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);
  const [selectedDoc, setSelectedDoc] = useState<Document | null>(null);
  const [options, setOptions] = useState<OCRRunOptions | null>(null);
  const [promptOverride, setPromptOverride] = useState<string | null>(null);
  const [job, setJob] = useState<OCRJobStatus | null>(null);
  const [runs, setRuns] = useState<OCRRun[]>([]);
  const [selectedJobId, setSelectedJobId] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastData | null>(null);
  const location = useLocation();
  const navigate = useNavigate();
  const rerunHandledRef = useRef(false);

  const jobRunning =
    job !== null && (job.status === "pending" || job.status === "in_progress");

  useEffect(() => {
    let cancelled = false;
    fetchOCRConfig()
      .then((cfg) => {
        if (cancelled) return;
        setConfig(cfg);
        setOptions((prev) => prev ?? { ...cfg.defaults });
      })
      .catch((err) => {
        console.error("Failed to load OCR config:", err);
        if (!cancelled) {
          setConfigError(
            "Could not reach the paperless-gpt backend. Check that the service is running, then reload."
          );
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Refresh after defaults were saved/reset so source markers stay truthful.
  const refreshConfig = useCallback(() => {
    fetchOCRConfig()
      .then(setConfig)
      .catch((err) => console.error("Failed to refresh OCR config:", err));
  }, []);

  const loadRuns = useCallback(async (documentId: number) => {
    const { runs } = await fetchOCRRuns(documentId);
    const completed = runs.filter((run) => run.status === "completed");
    setRuns(completed);
    return completed;
  }, []);

  const selectDocument = useCallback(
    async (doc: Document) => {
      setSelectedDoc(doc);
      setJob(null);
      setError(null);
      setSelectedJobId("");
      setRuns([]);
      try {
        const completed = await loadRuns(doc.id);
        if (completed.length > 0) {
          setSelectedJobId(completed[0].job_id);
        }
      } catch (err) {
        console.error("Failed to load runs for document:", err);
      }
    },
    [loadRuns]
  );

  // A "Re-run…" jump from the Activity tab preselects document and options.
  useEffect(() => {
    const state = location.state as RerunState | null;
    if (!state?.rerun || rerunHandledRef.current || !config) return;
    rerunHandledRef.current = true;
    const { rerun } = state;
    axios
      .get<Document>(`./api/documents/${rerun.documentId}`)
      .then(({ data }) => {
        setOptions(rerun.options);
        setPromptOverride(rerun.promptOverride);
        return selectDocument(data);
      })
      .catch(() => {
        setError(
          `Document #${rerun.documentId} (“${rerun.documentTitle}”) could not be loaded — it may have been replaced or deleted.`
        );
      });
    navigate(location.pathname, { replace: true });
  }, [location, config, navigate, selectDocument]);

  // Poll the active job until it reaches a terminal state.
  useEffect(() => {
    if (!jobRunning || !job || !selectedDoc) return;
    const interval = setInterval(async () => {
      try {
        const data = await fetchOCRJob(job.job_id);
        setJob(data);
        if (data.status === "completed") {
          const completed = await loadRuns(selectedDoc.id);
          setSelectedJobId(data.job_id);
          setJob(null);
          const run = completed.find((r) => r.job_id === data.job_id);
          setToast({
            kind: "success",
            message:
              run?.pdf_action === "replaced"
                ? "Run finished — the original was replaced with a searchable PDF."
                : "Run finished. Review the result below, then apply it.",
          });
        } else if (data.status === "failed") {
          setError(data.error || "The OCR run failed.");
          setJob(null);
          loadRuns(selectedDoc.id).catch(() => undefined);
        } else if (data.status === "cancelled") {
          setToast({ kind: "info", message: "Run cancelled. Nothing was changed." });
          setJob(null);
          loadRuns(selectedDoc.id).catch(() => undefined);
        }
      } catch (err) {
        console.error("Error polling OCR job:", err);
        setError(
          "Lost track of the OCR run — the backend may have restarted. Check the Activity tab."
        );
        setJob(null);
      }
    }, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [job, jobRunning, selectedDoc, loadRuns]);

  const startRun = async () => {
    if (!selectedDoc || !options) return;
    setError(null);
    try {
      const jobId = await submitOCRJob(selectedDoc.id, options, promptOverride);
      setJob({
        job_id: jobId,
        status: "pending",
        pages_done: 0,
        total_pages: 0,
      });
    } catch (err) {
      console.error("Failed to start OCR run:", err);
      const detail = axios.isAxiosError(err) && err.response?.data?.error;
      setError(detail || "Could not start the OCR run.");
    }
  };

  const tabs = [
    { key: "playground" as const, label: "Playground", to: "/ocr" },
    { key: "activity" as const, label: "Activity", to: "/ocr?tab=activity" },
  ];

  return (
    <div className="mx-auto max-w-7xl px-4 py-8 sm:px-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">OCR</h1>
          <p className="mt-1 text-sm text-muted">
            {config?.enabled
              ? `LLM-based text recognition — provider: ${config.provider}`
              : "LLM-based text recognition for your documents."}
          </p>
        </div>
        <nav aria-label="OCR sections" className="flex gap-1 rounded-lg border border-line bg-surface-2 p-1">
          {tabs.map((t) => (
            <Link
              key={t.key}
              to={t.to}
              aria-current={tab === t.key ? "page" : undefined}
              className={classNames(
                "rounded-md px-4 py-1.5 text-sm font-medium transition-colors duration-150 ease-out-quart",
                tab === t.key
                  ? "bg-surface text-ink shadow-card"
                  : "text-muted hover:text-ink"
              )}
            >
              {t.label}
            </Link>
          ))}
        </nav>
      </header>

      {configError && (
        <div role="alert" className="mt-6 rounded-lg border border-neg bg-neg-tint px-4 py-3 text-sm text-neg">
          {configError}
        </div>
      )}

      {config && !config.enabled && (
        <div className="mx-auto mt-16 max-w-xl rounded-lg border border-line bg-surface p-8">
          <h2 className="text-lg font-semibold">OCR is not configured yet</h2>
          <p className="mt-2 text-sm text-muted">
            Point paperless-gpt at an OCR provider and this page becomes a
            playground for LLM-based text recognition — run, compare, and apply
            OCR on any document, then automate it.
          </p>
          <pre className="mt-4 overflow-x-auto rounded-md border border-line bg-surface-2 p-3 text-xs leading-relaxed">
{`OCR_PROVIDER=llm
VISION_LLM_PROVIDER=ollama   # or openai, mistral, anthropic
VISION_LLM_MODEL=minicpm-v`}
          </pre>
          <a
            href="https://github.com/icereed/paperless-gpt#llm-based-ocr-compare-for-yourself"
            target="_blank"
            rel="noopener noreferrer"
            className="mt-4 inline-block text-sm font-medium text-primary hover:underline"
          >
            OCR setup documentation
          </a>
        </div>
      )}

      {config && config.enabled && tab === "activity" && (
        <div className="mt-6">
          <ActivityTab config={config} />
        </div>
      )}

      {config && config.enabled && options && tab === "playground" && (
        <div className="mt-6 space-y-4">
          <DocumentPicker
            selected={selectedDoc}
            onSelect={selectDocument}
            disabled={jobRunning}
          />

          {selectedDoc && (
            <RunOptionsPanel
              config={config}
              options={options}
              onChange={setOptions}
              promptOverride={promptOverride}
              onPromptOverrideChange={setPromptOverride}
              onStart={startRun}
              canStart={!jobRunning}
              running={jobRunning}
              onToast={(message) => setToast({ kind: "success", message })}
              onDefaultsChanged={refreshConfig}
            />
          )}

          {error && (
            <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-neg bg-neg-tint px-4 py-3 text-sm text-neg">
              <span>{error}</span>
              <Button size="sm" variant="secondary" onClick={() => setError(null)}>
                Dismiss
              </Button>
            </div>
          )}

          {jobRunning && job && (
            <div className="rounded-lg border border-line bg-surface p-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-sm font-medium" role="status">
                  {job.status === "pending"
                    ? "Waiting for a worker…"
                    : job.total_pages > 0
                      ? `Recognizing text — page ${job.pages_done} of ${job.total_pages}`
                      : "Recognizing text…"}
                </p>
                <Button variant="danger" size="sm" onClick={() => stopOCRJob(job.job_id)}>
                  <StopIcon className="h-4 w-4" aria-hidden="true" />
                  Stop
                </Button>
              </div>
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={Math.max(job.total_pages, 1)}
                aria-valuenow={job.pages_done}
                aria-label="Pages recognized"
                className="mt-4 h-1.5 overflow-hidden rounded-full bg-surface-2"
              >
                <div
                  className="h-full origin-left rounded-full bg-primary transition-transform duration-300 ease-out-quart"
                  style={{
                    transform: `scaleX(${
                      job.total_pages > 0 ? job.pages_done / job.total_pages : 0.05
                    })`,
                  }}
                />
              </div>
              <p className="mt-3 text-xs text-faint">
                Runs on the server — you can leave this page; the result lands in the Activity tab.
              </p>
            </div>
          )}

          {selectedDoc && !jobRunning && runs.length > 0 && selectedJobId && (
            <>
              <RunResults
                document={selectedDoc}
                runs={runs}
                selectedJobId={selectedJobId}
                onSelectRun={setSelectedJobId}
                onApplied={(message) => {
                  setToast({
                    kind: "success",
                    message,
                    action: { label: "Review in History", to: "/history" },
                  });
                  setSelectedDoc((prev) => prev); // content applied; keep context
                }}
              />
              <p className="rounded-lg border border-line bg-surface-2 px-4 py-3 text-sm text-muted">
                Happy with these results? <strong className="font-medium text-ink">Save as defaults</strong>{" "}
                (above) makes these options the auto-mode standard — then tag
                documents with{" "}
                <span className="whitespace-nowrap rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium text-ink">
                  {config.auto_tag}
                </span>{" "}
                and OCR runs hands-off. Check results anytime in the{" "}
                <Link to="/ocr?tab=activity" className="font-medium text-primary hover:underline">
                  Activity log
                </Link>
                .
              </p>
            </>
          )}

          {selectedDoc && !jobRunning && runs.length === 0 && (
            <p className="text-sm text-muted">
              No runs for this document yet — configure the options above and
              start the first one.
            </p>
          )}
        </div>
      )}

      {toast && <Toast toast={toast} onDismiss={() => setToast(null)} />}
    </div>
  );
};

export default OCR;

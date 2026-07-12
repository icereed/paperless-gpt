import {
  ArrowPathIcon,
  BoltIcon,
  CheckCircleIcon,
  ExclamationTriangleIcon,
  XCircleIcon,
} from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import Button from "../ui/Button";
import { OCRConfig, OCRRun, fetchOCRRuns, formatRunOptions, runDuration } from "./api";

interface ActivityTabProps {
  config: OCRConfig;
}

const statusMeta: Record<
  OCRRun["status"],
  { label: string; className: string; icon: React.ComponentType<React.SVGProps<SVGSVGElement>> }
> = {
  in_progress: { label: "Running", className: "bg-primary-tint text-ink", icon: ArrowPathIcon },
  completed: { label: "Completed", className: "bg-pos-tint text-pos", icon: CheckCircleIcon },
  failed: { label: "Failed", className: "bg-neg-tint text-neg", icon: XCircleIcon },
  cancelled: { label: "Cancelled", className: "bg-surface-2 text-muted", icon: XCircleIcon },
  interrupted: { label: "Interrupted", className: "bg-warn-tint text-warn", icon: ExclamationTriangleIcon },
};

/**
 * OCR Activity: the persisted log of every OCR Run — manual and auto — so a
 * hands-off user can verify what happened overnight and re-run anything with
 * adjusted options.
 */
const ActivityTab: React.FC<ActivityTabProps> = ({ config }) => {
  const [runs, setRuns] = useState<OCRRun[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();

  const load = useCallback(() => {
    let cancelled = false;
    fetchOCRRuns(undefined, 100)
      .then(({ runs }) => {
        if (cancelled) return;
        setRuns(runs);
        setError(null);
      })
      .catch((err) => {
        console.error("Failed to load OCR activity:", err);
        if (!cancelled) setError("Could not load the activity log.");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => load(), [load]);

  // Live refresh while anything is running.
  useEffect(() => {
    if (!runs?.some((run) => run.status === "in_progress")) return;
    const interval = setInterval(load, 4000);
    return () => clearInterval(interval);
  }, [runs, load]);

  const defaults = config.defaults;

  return (
    <div>
      <section
        aria-label="Auto-OCR status"
        className="rounded-lg border border-line bg-surface p-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <BoltIcon className="h-5 w-5 shrink-0 text-muted" aria-hidden="true" />
            <div className="min-w-0">
              <h2 className="text-sm font-medium">Auto-OCR</h2>
              <p className="mt-0.5 text-sm text-muted">
                Tag documents with{" "}
                <span className="whitespace-nowrap rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium text-ink">
                  {config.auto_tag}
                </span>{" "}
                in paperless-ngx and they are processed hands-off with the saved
                defaults: {defaults.process_mode},{" "}
                {defaults.limit_pages > 0 ? `max ${defaults.limit_pages} pages` : "all pages"}
                {defaults.upload_pdf
                  ? defaults.replace_original
                    ? ", PDF replaces original"
                    : ", PDF attached"
                  : ""}
                .
              </p>
            </div>
          </div>
          {typeof config.auto_queue_count === "number" && (
            <p className="shrink-0 text-sm tabular-nums text-muted" role="status">
              {config.auto_queue_count === 0
                ? "Queue empty"
                : `${config.auto_queue_count} in queue`}
            </p>
          )}
        </div>
      </section>

      {error && (
        <p role="alert" className="mt-4 rounded-md border border-neg bg-neg-tint px-3 py-2 text-sm text-neg">
          {error}
        </p>
      )}

      {!runs ? (
        <div className="mt-4 space-y-2" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-lg border border-line bg-surface" />
          ))}
          <span className="sr-only">Loading activity…</span>
        </div>
      ) : runs.length === 0 ? (
        <div className="mt-8 rounded-lg border border-line bg-surface p-8 text-center">
          <h2 className="text-base font-medium">No OCR runs yet</h2>
          <p className="mx-auto mt-2 max-w-md text-sm text-muted">
            Runs from the Playground and from Auto-OCR appear here with their
            options and outcome — including whether any original was replaced.
          </p>
        </div>
      ) : (
        <ul className="mt-4 space-y-2" aria-label="OCR runs">
          {runs.map((run) => {
            const meta = statusMeta[run.status] || statusMeta.completed;
            const Icon = meta.icon;
            const duration = runDuration(run);
            return (
              <li
                key={run.id}
                className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border border-line bg-surface px-4 py-3"
              >
                <span
                  className={classNames(
                    "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium",
                    meta.className
                  )}
                >
                  <Icon
                    className={classNames(
                      "h-3.5 w-3.5",
                      run.status === "in_progress" && "animate-spin"
                    )}
                    aria-hidden="true"
                  />
                  {meta.label}
                </span>
                <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 text-xs font-medium text-muted">
                  {run.trigger === "auto" ? "auto" : "manual"}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium" title={run.document_title}>
                    {run.document_title || `Document ${run.document_id}`}
                  </p>
                  <p className="truncate text-xs text-muted">
                    {new Date(run.started_at).toLocaleString()} ·{" "}
                    {formatRunOptions(run)}
                    {run.status === "in_progress" && run.total_pages > 0 &&
                      ` · page ${run.pages_done} of ${run.total_pages}`}
                    {run.status === "completed" && run.total_pages > 0 &&
                      ` · ${run.total_pages} ${run.total_pages === 1 ? "page" : "pages"}`}
                    {duration && ` · ${duration}`}
                  </p>
                  {run.pdf_action === "replaced" && (
                    <p className="mt-0.5 text-xs font-medium text-warn">
                      Original replaced with searchable PDF
                    </p>
                  )}
                  {(run.pdf_action === "skipped" || run.pdf_action === "failed") &&
                    run.pdf_detail && (
                      <p className="mt-0.5 truncate text-xs text-warn" title={run.pdf_detail}>
                        PDF {run.pdf_action}: {run.pdf_detail}
                      </p>
                    )}
                  {run.error && run.status !== "completed" && (
                    <p className="mt-0.5 truncate text-xs text-neg" title={run.error}>
                      {run.error}
                    </p>
                  )}
                </div>
                <Button
                  size="sm"
                  variant="secondary"
                  className="shrink-0"
                  title="Open in the Playground with these options prefilled"
                  onClick={() =>
                    navigate("/ocr", {
                      state: {
                        rerun: {
                          documentId: run.document_id,
                          documentTitle: run.document_title,
                          options: {
                            limit_pages: run.limit_pages,
                            process_mode: run.process_mode || "image",
                            upload_pdf: run.upload_pdf,
                            replace_original: run.replace_original,
                            copy_metadata: run.copy_metadata,
                          },
                          promptOverride: run.prompt_overridden
                            ? run.prompt_override || null
                            : null,
                        },
                      },
                    })
                  }
                >
                  Re-run…
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
};

export default ActivityTab;

import {
  ArrowPathIcon,
  CheckCircleIcon,
  DocumentIcon,
  ExclamationTriangleIcon,
} from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useEffect, useMemo, useState } from "react";
import { Document } from "../../DocumentProcessor";
import Button from "../ui/Button";
import Modal from "../ui/Modal";
import {
  OCRPage,
  OCRRun,
  applyContent,
  fetchOCRPages,
  formatRunOptions,
  reOCRPage,
} from "./api";

interface RunResultsProps {
  document: Document;
  runs: OCRRun[]; // completed runs of this document, newest first
  selectedJobId: string;
  onSelectRun: (jobId: string) => void;
  onApplied: (message: string) => void;
}

interface PageImageProps {
  documentId: number;
  pageIndex: number;
  singlePage: boolean;
}

const PageImage: React.FC<PageImageProps> = ({ documentId, pageIndex, singlePage }) => {
  const [failed, setFailed] = useState(false);
  if (failed) {
    return (
      <div className="flex h-48 items-center justify-center rounded border border-line bg-surface-2">
        <DocumentIcon className="h-8 w-8 text-faint" aria-hidden="true" />
      </div>
    );
  }
  return (
    <img
      src={`./api/documents/${documentId}/pages/${pageIndex}/image`}
      alt={singlePage ? "Scan" : `Scan of page ${pageIndex + 1}`}
      loading="lazy"
      className="w-full rounded border border-line bg-surface object-contain shadow-card"
      onError={() => setFailed(true)}
    />
  );
};

/**
 * The result of an OCR Run, page by page: the scan next to the recognized
 * (editable) text, per-page re-OCR, a run switcher with side-by-side compare,
 * and the apply step that writes the text back to paperless-ngx.
 */
const RunResults: React.FC<RunResultsProps> = ({
  document,
  runs,
  selectedJobId,
  onSelectRun,
  onApplied,
}) => {
  const [pages, setPages] = useState<OCRPage[] | null>(null);
  const [editedTexts, setEditedTexts] = useState<Record<number, string>>({});
  const [compareJobId, setCompareJobId] = useState<string>("");
  const [comparePages, setComparePages] = useState<OCRPage[] | null>(null);
  const [reOcrPending, setReOcrPending] = useState<Set<number>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const [showApplyConfirm, setShowApplyConfirm] = useState(false);
  const [applying, setApplying] = useState(false);

  const selectedRun = runs.find((run) => run.job_id === selectedJobId);
  const newestJobId = runs[0]?.job_id;
  const isNewestRun = selectedJobId === newestJobId;

  useEffect(() => {
    if (!selectedJobId) return;
    let cancelled = false;
    setPages(null);
    setEditedTexts({});
    fetchOCRPages(document.id, selectedJobId)
      .then(({ pages }) => {
        if (!cancelled) setPages(pages);
      })
      .catch((err) => {
        console.error("Failed to fetch OCR pages:", err);
        if (!cancelled) setError("Could not load the run's pages.");
      });
    return () => {
      cancelled = true;
    };
  }, [document.id, selectedJobId]);

  useEffect(() => {
    if (!compareJobId) {
      setComparePages(null);
      return;
    }
    let cancelled = false;
    fetchOCRPages(document.id, compareJobId)
      .then(({ pages }) => {
        if (!cancelled) setComparePages(pages);
      })
      .catch((err) => console.error("Failed to fetch compare pages:", err));
    return () => {
      cancelled = true;
    };
  }, [document.id, compareJobId]);

  const currentText = (page: OCRPage) =>
    editedTexts[page.pageIndex] ?? page.text;

  const combinedText = useMemo(
    () => (pages || []).map(currentText).join("\n\n"),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [pages, editedTexts]
  );

  const handleReOCR = async (pageIndex: number) => {
    setReOcrPending((prev) => new Set(prev).add(pageIndex));
    setError(null);
    try {
      const refreshed = await reOCRPage(document.id, pageIndex, selectedJobId);
      setPages((prev) =>
        (prev || []).map((p) => (p.pageIndex === pageIndex ? refreshed : p))
      );
      setEditedTexts((prev) => {
        const next = { ...prev };
        delete next[pageIndex];
        return next;
      });
    } catch (err) {
      console.error("Re-OCR failed:", err);
      setError(`Re-OCR of page ${pageIndex + 1} failed.`);
    } finally {
      setReOcrPending((prev) => {
        const next = new Set(prev);
        next.delete(pageIndex);
        return next;
      });
    }
  };

  const handleApply = async () => {
    setApplying(true);
    setError(null);
    try {
      await applyContent(document, combinedText);
      setShowApplyConfirm(false);
      onApplied(
        `Text applied to “${document.title}” — revert anytime in History.`
      );
    } catch (err) {
      console.error("Applying content failed:", err);
      setError("Applying the text failed — nothing was written.");
    } finally {
      setApplying(false);
    }
  };

  if (!selectedRun) return null;

  const editedCount = Object.keys(editedTexts).length;
  const singlePage = (pages || []).length === 1 && selectedRun.process_mode === "whole_pdf";
  const compareRun = runs.find((run) => run.job_id === compareJobId);

  return (
    <section aria-label="OCR result" className="mt-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h2 className="text-lg font-semibold">Result</h2>
          {runs.length > 1 ? (
            <>
              <select
                value={selectedJobId}
                onChange={(e) => {
                  onSelectRun(e.target.value);
                  if (e.target.value === compareJobId) setCompareJobId("");
                }}
                aria-label="Select run"
                className="h-8 rounded-md border border-line bg-surface px-2 pr-7 text-sm"
              >
                {runs.map((run, i) => (
                  <option key={run.job_id} value={run.job_id}>
                    Run {runs.length - i} ·{" "}
                    {new Date(run.started_at).toLocaleTimeString()} ·{" "}
                    {run.prompt_overridden ? "custom prompt" : "default prompt"}
                  </option>
                ))}
              </select>
              <select
                value={compareJobId}
                onChange={(e) => setCompareJobId(e.target.value)}
                aria-label="Compare with run"
                className="h-8 rounded-md border border-line bg-surface px-2 pr-7 text-sm"
              >
                <option value="">No comparison</option>
                {runs
                  .filter((run) => run.job_id !== selectedJobId)
                  .map((run, i, arr) => (
                    <option key={run.job_id} value={run.job_id}>
                      vs. run {arr.length - i} ·{" "}
                      {new Date(run.started_at).toLocaleTimeString()}
                    </option>
                  ))}
              </select>
            </>
          ) : (
            <span className="text-sm text-muted">{formatRunOptions(selectedRun)}</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-faint">
            {editedCount > 0 &&
              `${editedCount} ${editedCount === 1 ? "page" : "pages"} edited · `}
            {combinedText.length.toLocaleString()} chars
          </span>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setShowApplyConfirm(true)}
            disabled={!pages || pages.length === 0}
          >
            Apply text to document
          </Button>
        </div>
      </div>

      {selectedRun.pdf_action === "attached" && (
        <p className="mt-2 flex items-center gap-1.5 text-sm text-pos">
          <CheckCircleIcon className="h-4 w-4" aria-hidden="true" />
          A searchable PDF was attached as a new document during this run.
        </p>
      )}
      {selectedRun.pdf_action === "replaced" && (
        <p className="mt-2 flex items-center gap-1.5 text-sm text-pos">
          <CheckCircleIcon className="h-4 w-4" aria-hidden="true" />
          The original was replaced with a searchable PDF during this run.
        </p>
      )}
      {(selectedRun.pdf_action === "skipped" || selectedRun.pdf_action === "failed") && (
        <p className="mt-2 flex items-center gap-1.5 text-sm text-warn">
          <ExclamationTriangleIcon className="h-4 w-4" aria-hidden="true" />
          Searchable PDF {selectedRun.pdf_action}: {selectedRun.pdf_detail}
        </p>
      )}

      {error && (
        <p role="alert" className="mt-3 rounded-md border border-neg bg-neg-tint px-3 py-2 text-sm text-neg">
          {error}
        </p>
      )}

      {!pages ? (
        <div className="mt-4 space-y-4" aria-busy="true">
          {[0, 1].map((i) => (
            <div key={i} className="h-48 animate-pulse rounded-lg border border-line bg-surface" />
          ))}
          <span className="sr-only">Loading pages…</span>
        </div>
      ) : (
        <div className="mt-4 space-y-4">
          {pages.map((page) => {
            const comparePage = comparePages?.find(
              (p) => p.pageIndex === page.pageIndex
            );
            return (
              <article
                key={page.pageIndex}
                aria-label={singlePage ? "Document" : `Page ${page.pageIndex + 1}`}
                className="rounded-lg border border-line bg-surface p-4"
              >
                <div className="flex items-center justify-between gap-2">
                  <h3 className="text-sm font-medium text-muted">
                    {singlePage ? "Whole document" : `Page ${page.pageIndex + 1}`}
                    {page.ocrLimitHit && (
                      <span className="ml-2 rounded-full bg-warn-tint px-2 py-0.5 text-xs font-medium text-warn">
                        output limit hit — text may be truncated
                      </span>
                    )}
                  </h3>
                  {isNewestRun && !singlePage && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => handleReOCR(page.pageIndex)}
                      loading={reOcrPending.has(page.pageIndex)}
                      title="Run OCR again for this page only"
                    >
                      {!reOcrPending.has(page.pageIndex) && (
                        <ArrowPathIcon className="h-3.5 w-3.5" aria-hidden="true" />
                      )}
                      Re-OCR page
                    </Button>
                  )}
                </div>

                <div
                  className={classNames(
                    "mt-3 grid gap-4",
                    compareJobId ? "lg:grid-cols-3" : "lg:grid-cols-2"
                  )}
                >
                  <div className="min-w-0">
                    <PageImage
                      documentId={document.id}
                      pageIndex={singlePage ? 0 : page.pageIndex}
                      singlePage={singlePage}
                    />
                  </div>
                  <div className="min-w-0">
                    <label className="mb-1 block text-xs font-medium text-faint">
                      {compareJobId ? "This run (editable)" : "Recognized text (editable)"}
                    </label>
                    <textarea
                      value={currentText(page)}
                      onChange={(e) =>
                        setEditedTexts((prev) => ({
                          ...prev,
                          [page.pageIndex]: e.target.value,
                        }))
                      }
                      rows={14}
                      aria-label={
                        singlePage
                          ? "Recognized text"
                          : `Recognized text of page ${page.pageIndex + 1}`
                      }
                      spellCheck={false}
                      className={classNames(
                        "w-full rounded-md border bg-surface px-3 py-2 text-sm leading-relaxed",
                        editedTexts[page.pageIndex] !== undefined
                          ? "border-primary"
                          : "border-line"
                      )}
                    />
                  </div>
                  {compareJobId && (
                    <div className="min-w-0">
                      <label className="mb-1 block text-xs font-medium text-faint">
                        Comparison run{" "}
                        {compareRun?.prompt_overridden ? "(custom prompt)" : ""}
                      </label>
                      <div className="h-[calc(100%-1.5rem)] min-h-48 overflow-y-auto whitespace-pre-wrap rounded-md border border-line bg-surface-2 px-3 py-2 text-sm leading-relaxed text-muted">
                        {comparePage ? comparePage.text : "No text for this page in the comparison run."}
                      </div>
                    </div>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      )}

      <Modal
        open={showApplyConfirm}
        onClose={() => !applying && setShowApplyConfirm(false)}
        title={`Apply text to “${document.title}”?`}
        size="lg"
      >
        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4 text-sm">
          <p className="text-muted">
            The document content in paperless-ngx (
            {(document.content || "").length.toLocaleString()} chars) is
            replaced by the recognized text ({combinedText.length.toLocaleString()}{" "}
            chars{editedCount > 0 ? `, ${editedCount} ${editedCount === 1 ? "page" : "pages"} hand-edited` : ""}).
          </p>
          <p className="mt-3 text-xs text-faint">
            The change is recorded and can be reverted on the History page.
          </p>
        </div>
        <div className="flex shrink-0 justify-end gap-2 border-t border-line px-6 py-4">
          <Button variant="secondary" onClick={() => setShowApplyConfirm(false)} disabled={applying}>
            Cancel
          </Button>
          <Button variant="primary" onClick={handleApply} loading={applying}>
            Apply text
          </Button>
        </div>
      </Modal>
    </section>
  );
};

export default RunResults;

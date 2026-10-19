import {
  ArrowTopRightOnSquareIcon,
  CheckCircleIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  DocumentIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useEffect, useId, useRef, useState } from "react";
import { DocumentSuggestion, TagOption } from "../../DocumentProcessor";
import Button from "../ui/Button";
import Modal from "../ui/Modal";
import {
  Decision,
  FieldKey,
  SuggestionEditHandlers,
  countChanges,
} from "./fields";
import SuggestionFields from "./SuggestionFields";

interface FocusReviewProps {
  items: DocumentSuggestion[];
  index: number;
  decisions: Record<number, Decision>;
  excludedMap: Record<number, Set<FieldKey>>;
  availableTags: TagOption[];
  handlers: SuggestionEditHandlers;
  onToggleField: (docId: number, key: FieldKey) => void;
  onApply: (docId: number) => void;
  onSkip: (docId: number) => void;
  onNavigate: (index: number) => void;
  onClose: () => void;
  applyingIds: Set<number>;
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.isContentEditable
  );
}

type SourceView = "document" | "text";

const sourceViews: readonly (readonly [SourceView, string])[] = [
  ["document", "Document"],
  ["text", "Extracted text"],
];

/**
 * Reads the error out of a preview frame that loaded the proxy's JSON error
 * instead of the document. A frame has no load-failure event for HTTP errors,
 * but the preview is same-origin, so its content can be inspected.
 */
function previewFrameError(frame: HTMLIFrameElement): string | null {
  const doc = frame.contentDocument;
  if (!doc || doc.contentType !== "application/json") return null;
  try {
    return JSON.parse(doc.body.innerText).error || "The document couldn't be loaded.";
  } catch {
    return "The document couldn't be loaded.";
  }
}

/**
 * One document at a time: the document and its OCR text on the left ("show
 * the work"), the field diffs on the right, and a keyboard-first flow (j/k/a/s).
 */
const FocusReview: React.FC<FocusReviewProps> = ({
  items,
  index,
  decisions,
  excludedMap,
  availableTags,
  handlers,
  onToggleField,
  onApply,
  onSkip,
  onNavigate,
  onClose,
  applyingIds,
}) => {
  const item = items[index];
  // The view is kept while navigating, so reviewing a batch against the
  // documents doesn't need a click per document.
  const [sourceView, setSourceView] = useState<SourceView>("document");
  // A preview failure is forgotten as soon as another document is shown, so
  // coming back to the document tries again; a failure can be transient
  // (paperless-ngx briefly unreachable) and shouldn't hide the document for the
  // whole review. Resetting during render rather than in an effect avoids
  // rendering the stale error first.
  const [previewFailure, setPreviewFailure] = useState<{
    documentId: number;
    message: string;
  } | null>(null);
  const [shownDocumentId, setShownDocumentId] = useState(item?.id);
  if (item && item.id !== shownDocumentId) {
    setShownDocumentId(item.id);
    setPreviewFailure(null);
  }
  // The document is only fetched once its tab has been shown, so reviewing a
  // batch on the text tab doesn't download every document.
  const [framedDocumentId, setFramedDocumentId] = useState<number | null>(
    null
  );
  if (item && sourceView === "document" && framedDocumentId !== item.id) {
    setFramedDocumentId(item.id);
  }
  const [paperlessUrl, setPaperlessUrl] = useState("");
  const panelRef = useRef<HTMLDivElement>(null);
  const tabIdPrefix = useId();
  const tabId = (view: SourceView) => `${tabIdPrefix}-tab-${view}`;
  const tabPanelId = (view: SourceView) => `${tabIdPrefix}-panel-${view}`;

  useEffect(() => {
    fetch("./api/paperless-url")
      .then((response) => (response.ok ? response.json() : { url: "" }))
      .then(({ url }) => setPaperlessUrl(url || ""))
      .catch(() => setPaperlessUrl(""));
  }, []);

  if (!item) return null;

  const previewUrl = `./api/documents/${item.id}/preview`;
  const previewError =
    previewFailure?.documentId === item.id ? previewFailure.message : null;

  const decision = decisions[item.id] || "pending";
  const excluded = excludedMap[item.id] || new Set<FieldKey>();
  const changeCount = countChanges(item, excluded);
  const applying = applyingIds.has(item.id);

  const goPrev = () => onNavigate(index > 0 ? index - 1 : items.length - 1);
  const goNext = () => onNavigate(index < items.length - 1 ? index + 1 : 0);

  // Arrow keys on a focused tab move between the tabs, as for any tab list,
  // instead of reaching handleKeyDown and switching to another document.
  const handleTabKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const current = sourceViews.findIndex(([view]) => view === sourceView);
    let next: number;
    switch (e.key) {
      case "ArrowLeft":
        next = (current - 1 + sourceViews.length) % sourceViews.length;
        break;
      case "ArrowRight":
        next = (current + 1) % sourceViews.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = sourceViews.length - 1;
        break;
      default:
        return;
    }
    e.preventDefault();
    e.stopPropagation();
    const [view] = sourceViews[next];
    setSourceView(view);
    document.getElementById(tabId(view))?.focus();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (isEditableTarget(e.target)) return;
    switch (e.key) {
      case "j":
      case "ArrowRight":
        e.preventDefault();
        goNext();
        break;
      case "k":
      case "ArrowLeft":
        e.preventDefault();
        goPrev();
        break;
      case "a":
        e.preventDefault();
        if (decision === "pending" && !applying) {
          onApply(item.id);
          goNext();
        }
        break;
      case "s":
        e.preventDefault();
        if (decision === "pending" && !applying) {
          onSkip(item.id);
          goNext();
        }
        break;
      case "e": {
        e.preventDefault();
        const input = panelRef.current?.querySelector<HTMLInputElement>(
          "input[type='text']:not(:disabled)"
        );
        input?.focus();
        input?.select();
        break;
      }
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      size="focus"
      hideTitle
      title={`Focus view: ${item.original_document.title}`}
      initialFocus={panelRef}
    >
      {/* Focus starts on the panel itself (not the first input) so j/k/a/s work immediately. */}
      <div
        ref={panelRef}
        tabIndex={-1}
        className="flex min-h-0 flex-1 flex-col outline-none"
        onKeyDown={handleKeyDown}
      >
        <header className="flex shrink-0 items-center justify-between gap-4 border-b border-line px-5 py-3">
          <div className="flex min-w-0 items-center gap-3">
            <h3
              className="truncate text-sm font-medium"
              title={item.original_document.title}
            >
              {item.original_document.title}
            </h3>
            {decision === "applied" && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-pos-tint px-2 py-0.5 text-xs font-medium text-pos">
                <CheckCircleIcon className="h-3.5 w-3.5" aria-hidden="true" />
                Applied
              </span>
            )}
            {(decision === "skipped" || decision === "discarded") && (
              <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 text-xs font-medium text-muted">
                {decision === "skipped" ? "Skipped" : "Discarded"}
              </span>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {paperlessUrl && (
              <a
                href={`${paperlessUrl}/documents/${item.id}/details`}
                target="_blank"
                rel="noopener noreferrer"
                className="mr-2 inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted hover:bg-surface-2 hover:text-ink"
              >
                Open in paperless-ngx
                <ArrowTopRightOnSquareIcon className="h-3.5 w-3.5" aria-hidden="true" />
              </a>
            )}
            <span className="mr-2 text-xs tabular-nums text-faint">
              Document {index + 1} of {items.length}
            </span>
            <button
              type="button"
              onClick={goPrev}
              aria-label="Previous document"
              className="rounded-md p-1.5 text-muted hover:bg-surface-2 hover:text-ink"
            >
              <ChevronLeftIcon className="h-4 w-4" aria-hidden="true" />
            </button>
            <button
              type="button"
              onClick={goNext}
              aria-label="Next document"
              className="rounded-md p-1.5 text-muted hover:bg-surface-2 hover:text-ink"
            >
              <ChevronRightIcon className="h-4 w-4" aria-hidden="true" />
            </button>
            <button
              type="button"
              onClick={onClose}
              aria-label="Close focus view"
              className="ml-1 rounded-md p-1.5 text-muted hover:bg-surface-2 hover:text-ink"
            >
              <XMarkIcon className="h-4 w-4" aria-hidden="true" />
            </button>
          </div>
        </header>

        <div className="flex min-h-0 flex-1">
          <div className="hidden min-h-0 w-1/2 flex-col border-r border-line bg-surface-2 md:flex">
            <div className="flex shrink-0 items-center justify-between gap-2 border-b border-line px-4 py-2">
              <div
                className="flex gap-1.5"
                role="tablist"
                aria-label="Source"
                onKeyDown={handleTabKeyDown}
              >
                {sourceViews.map(([view, label]) => (
                  <button
                    key={view}
                    id={tabId(view)}
                    type="button"
                    role="tab"
                    aria-selected={sourceView === view}
                    aria-controls={tabPanelId(view)}
                    tabIndex={sourceView === view ? 0 : -1}
                    onClick={() => setSourceView(view)}
                    className={classNames(
                      "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                      sourceView === view
                        ? "bg-primary-tint text-ink"
                        : "text-muted hover:text-ink"
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>
              {sourceView === "document" && !previewError && (
                <a
                  href={previewUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-muted hover:text-ink"
                >
                  Open in new tab
                  <ArrowTopRightOnSquareIcon className="h-3.5 w-3.5" aria-hidden="true" />
                </a>
              )}
            </div>
            {/* Hidden rather than unmounted, so switching back doesn't reload the document. */}
            <div
              id={tabPanelId("document")}
              role="tabpanel"
              aria-labelledby={tabId("document")}
              className={classNames(
                "min-h-0 flex-1",
                sourceView !== "document" && "hidden"
              )}
            >
              {previewError ? (
                <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
                  <DocumentIcon className="h-10 w-10 text-faint" aria-hidden="true" />
                  <p className="text-sm text-muted">{previewError}</p>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setPreviewFailure(null)}
                  >
                    Try again
                  </Button>
                  {paperlessUrl && (
                    <a
                      href={`${paperlessUrl}/documents/${item.id}/details`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-primary hover:underline"
                    >
                      Open it in paperless-ngx
                    </a>
                  )}
                </div>
              ) : framedDocumentId === item.id && (
                <iframe
                  key={item.id}
                  src={previewUrl}
                  title={`Document “${item.original_document.title}”`}
                  className="h-full w-full bg-surface"
                  onLoad={(e) => {
                    const error = previewFrameError(e.currentTarget);
                    if (error) {
                      setPreviewFailure({ documentId: item.id, message: error });
                    }
                  }}
                />
              )}
            </div>
            <div
              id={tabPanelId("text")}
              role="tabpanel"
              aria-labelledby={tabId("text")}
              // Focusable so the text can be scrolled from the keyboard; the
              // panel has nothing else to focus.
              tabIndex={0}
              className={classNames(
                "min-h-0 flex-1 overflow-y-auto p-4",
                sourceView !== "text" && "hidden"
              )}
            >
              <h4 className="text-xs font-medium text-muted">
                What the AI read
              </h4>
              <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-ink">
                {item.original_document.content || "No text extracted."}
              </p>
            </div>
          </div>

          <div className="flex min-h-0 w-full flex-col md:w-1/2">
            <div className="min-h-0 flex-1 overflow-y-auto px-5 py-2">
              <SuggestionFields
                suggestion={item}
                availableTags={availableTags}
                excluded={excluded}
                onToggleField={onToggleField}
                handlers={handlers}
                disabled={decision !== "pending" || applying}
              />
            </div>
            <footer className="flex shrink-0 items-center justify-between gap-3 border-t border-line px-5 py-3">
              <p className="hidden text-xs text-faint sm:block" aria-hidden="true">
                <kbd className="rounded border border-line bg-surface-2 px-1">j</kbd>/
                <kbd className="rounded border border-line bg-surface-2 px-1">k</kbd>{" "}
                documents ·{" "}
                <kbd className="rounded border border-line bg-surface-2 px-1">a</kbd>{" "}
                apply ·{" "}
                <kbd className="rounded border border-line bg-surface-2 px-1">s</kbd>{" "}
                skip ·{" "}
                <kbd className="rounded border border-line bg-surface-2 px-1">esc</kbd>{" "}
                close
              </p>
              {decision === "pending" ? (
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={applying}
                    onClick={() => {
                      onSkip(item.id);
                      goNext();
                    }}
                  >
                    Skip
                  </Button>
                  <Button
                    size="sm"
                    variant="primary"
                    loading={applying}
                    onClick={() => {
                      onApply(item.id);
                      goNext();
                    }}
                  >
                    Apply
                    {changeCount > 0 && (
                      <span className="tabular-nums">({changeCount})</span>
                    )}
                  </Button>
                </div>
              ) : (
                <span className="text-xs text-faint">
                  Decided — use j/k to keep reviewing.
                </span>
              )}
            </footer>
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default FocusReview;

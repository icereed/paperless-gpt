import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import axios from "axios";
import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  DocumentSuggestion,
  SuggestionJobFailedDocument,
  TagOption,
} from "../DocumentProcessor";
import SuggestionCard from "./SuggestionCard";
import Button from "./ui/Button";
import ConfirmDialog from "./ui/ConfirmDialog";
import ApplySummaryDialog from "./review/ApplySummaryDialog";
import FocusReview from "./review/FocusReview";
import {
  Decision,
  FieldKey,
  SuggestionEditHandlers,
  buildUpdatePayload,
  countChanges,
} from "./review/fields";

interface SuggestionsReviewProps {
  suggestions: DocumentSuggestion[];
  availableTags: TagOption[];
  filterTag: string | null;
  failedDocuments: SuggestionJobFailedDocument[];
  onRetryFailed: () => void;
  onFinished: (appliedCount: number, fieldChanges: number) => void;
  onDiscard: () => void;
}

const SuggestionsReview: React.FC<SuggestionsReviewProps> = ({
  suggestions,
  availableTags,
  filterTag,
  failedDocuments,
  onRetryFailed,
  onFinished,
  onDiscard,
}) => {
  const [items, setItems] = useState<DocumentSuggestion[]>(suggestions);
  const [decisions, setDecisions] = useState<Record<number, Decision>>({});
  const [excludedMap, setExcludedMap] = useState<
    Record<number, Set<FieldKey>>
  >({});
  const [applyingIds, setApplyingIds] = useState<Set<number>>(new Set());
  const [focusIndex, setFocusIndex] = useState<number | null>(null);
  const [showSummary, setShowSummary] = useState(false);
  const [showDiscardConfirm, setShowDiscardConfirm] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const statsRef = useRef({ docs: 0, fields: 0 });
  const finishedRef = useRef(false);

  // A retry job can append documents after mount.
  useEffect(() => {
    setItems((prev) => {
      const known = new Set(prev.map((item) => item.id));
      const added = suggestions.filter((s) => !known.has(s.id));
      return added.length > 0 ? [...prev, ...added] : prev;
    });
  }, [suggestions]);

  const updateItem = useCallback(
    (docId: number, update: (item: DocumentSuggestion) => DocumentSuggestion) => {
      setItems((prev) =>
        prev.map((item) => (item.id === docId ? update(item) : item))
      );
    },
    []
  );

  const handlers: SuggestionEditHandlers = {
    onTitleChange: (docId, title) =>
      updateItem(docId, (item) => ({ ...item, suggested_title: title })),
    onTagAddition: (docId, tag) =>
      updateItem(docId, (item) => ({
        ...item,
        suggested_tags: [...(item.suggested_tags || []), tag.name],
      })),
    onTagDeletion: (docId, index) =>
      updateItem(docId, (item) => ({
        ...item,
        suggested_tags: item.suggested_tags?.filter((_, i) => i !== index),
      })),
    onCorrespondentChange: (docId, correspondent) =>
      updateItem(docId, (item) => ({
        ...item,
        suggested_correspondent: correspondent,
      })),
    onDocumentTypeChange: (docId, documentType) =>
      updateItem(docId, (item) => ({
        ...item,
        suggested_document_type: documentType,
      })),
    onCreatedDateChange: (docId, createdDate) =>
      updateItem(docId, (item) => ({
        ...item,
        suggested_created_date: createdDate,
      })),
    onCustomFieldSuggestionToggle: (docId, fieldId) =>
      updateItem(docId, (item) => ({
        ...item,
        suggested_custom_fields: item.suggested_custom_fields?.map((cf) =>
          cf.id === fieldId ? { ...cf, isSelected: !cf.isSelected } : cf
        ),
      })),
  };

  const handleToggleField = (docId: number, key: FieldKey) => {
    setExcludedMap((prev) => {
      const current = new Set(prev[docId] || []);
      if (current.has(key)) {
        current.delete(key);
      } else {
        current.add(key);
      }
      return { ...prev, [docId]: current };
    });
  };

  const pendingItems = items.filter(
    (item) => (decisions[item.id] || "pending") === "pending"
  );
  const decidedCount = items.length - pendingItems.length;
  const skippedCount = items.filter(
    (item) => decisions[item.id] === "skipped"
  ).length;

  // Fire onFinished exactly once, from an effect (not inside a state updater,
  // which React may replay). It triggers when every item has been decided.
  useEffect(() => {
    if (finishedRef.current || items.length === 0) return;
    const allDecided = items.every(
      (item) => (decisions[item.id] || "pending") !== "pending"
    );
    if (allDecided) {
      finishedRef.current = true;
      onFinished(statsRef.current.docs, statsRef.current.fields);
    }
  }, [items, decisions, onFinished]);

  const applyDocuments = async (docsToApply: DocumentSuggestion[]) => {
    setError(null);
    const ids = docsToApply.map((doc) => doc.id);
    // Track applying ids incrementally so concurrent single-doc applies don't
    // clear each other's loading state.
    setApplyingIds((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => next.add(id));
      return next;
    });
    try {
      const payload = docsToApply.map((item) =>
        buildUpdatePayload(item, excludedMap[item.id] || new Set())
      );
      await axios.patch("./api/update-documents", payload);

      statsRef.current.docs += docsToApply.length;
      statsRef.current.fields += docsToApply.reduce(
        (sum, item) =>
          sum + countChanges(item, excludedMap[item.id] || new Set()),
        0
      );

      setDecisions((prev) => {
        const next = { ...prev };
        docsToApply.forEach((doc) => {
          next[doc.id] = "applied";
        });
        return next;
      });
      setShowSummary(false);
    } catch (err) {
      console.error("Error updating documents:", err);
      setError(
        "Applying the changes failed — nothing was marked as done. Check the backend connection and try again."
      );
    } finally {
      setApplyingIds((prev) => {
        const next = new Set(prev);
        ids.forEach((id) => next.delete(id));
        return next;
      });
    }
  };

  const handleApplyOne = (docId: number) => {
    const item = items.find((i) => i.id === docId);
    if (item) applyDocuments([item]);
  };

  const handleSkip = (docId: number) => {
    setDecisions((prev) => ({ ...prev, [docId]: "skipped" as Decision }));
  };

  const handleDiscard = () => {
    // Only confirm when there is something undecided to discard. `edited` stays
    // true after edits are applied, so gating on it would show a misleading
    // "0 undecided suggestions will be discarded" dialog.
    if (pendingItems.length > 0) {
      setShowDiscardConfirm(true);
    } else if (!finishedRef.current) {
      finishedRef.current = true;
      onFinished(statsRef.current.docs, statsRef.current.fields);
    }
  };

  const focusItems = items;

  return (
    // "suggestions-review" is a stable hook for E2E tests; keep it when restyling.
    <section aria-label="Review suggestions" className="suggestions-review">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Review suggestions</h1>
          <p className="mt-1 text-sm text-muted">
            Check each change (− current, + suggested), edit where needed, then
            apply per document or all at once.
          </p>
        </div>
      </header>

      {failedDocuments.length > 0 && (
        <div
          role="alert"
          className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-warn bg-warn-tint px-4 py-3 text-sm"
        >
          <span className="flex min-w-0 items-center gap-2">
            <ExclamationTriangleIcon
              className="h-4 w-4 shrink-0 text-warn"
              aria-hidden="true"
            />
            <span>
              {failedDocuments.length}{" "}
              {failedDocuments.length === 1 ? "document" : "documents"} could
              not be processed:{" "}
              {failedDocuments.map((f) => f.document_title).join(", ")}
            </span>
          </span>
          <Button size="sm" variant="secondary" onClick={onRetryFailed}>
            Retry failed
          </Button>
        </div>
      )}

      {error && (
        <div
          role="alert"
          className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-neg bg-neg-tint px-4 py-3 text-sm text-neg"
        >
          <span>{error}</span>
          <Button size="sm" variant="secondary" onClick={() => setError(null)}>
            Dismiss
          </Button>
        </div>
      )}

      <div className="mt-6 grid items-start gap-4 md:grid-cols-2 xl:grid-cols-3">
        {items.map((item, index) => (
          <SuggestionCard
            key={item.id}
            suggestion={item}
            availableTags={availableTags}
            decision={decisions[item.id] || "pending"}
            excluded={excludedMap[item.id] || new Set()}
            onToggleField={handleToggleField}
            handlers={handlers}
            onApply={handleApplyOne}
            onSkip={handleSkip}
            onOpenFocus={() => setFocusIndex(index)}
            applying={applyingIds.has(item.id)}
          />
        ))}
      </div>

      <div className="sticky bottom-0 z-sticky -mx-4 mt-6 border-t border-line bg-surface sm:-mx-6">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
          <p className="text-sm tabular-nums text-muted" role="status">
            {decidedCount} of {items.length} decided
            {skippedCount > 0 && ` · ${skippedCount} skipped`}
          </p>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={handleDiscard}>
              {pendingItems.length > 0 ? "Close review" : "Done"}
            </Button>
            {pendingItems.length > 0 && (
              <Button
                variant="primary"
                onClick={() => setShowSummary(true)}
                disabled={applyingIds.size > 0}
              >
                Apply remaining ({pendingItems.length})
              </Button>
            )}
          </div>
        </div>
      </div>

      {showSummary && (
        <ApplySummaryDialog
          open={showSummary}
          items={pendingItems}
          excludedMap={excludedMap}
          filterTag={filterTag}
          applying={applyingIds.size > 0}
          onConfirm={() => applyDocuments(pendingItems)}
          onCancel={() => setShowSummary(false)}
        />
      )}

      <ConfirmDialog
        open={showDiscardConfirm}
        title="Close review?"
        body={
          statsRef.current.docs > 0
            ? `${statsRef.current.docs} applied ${statsRef.current.docs === 1 ? "document keeps" : "documents keep"} their changes. The remaining ${pendingItems.length} undecided ${pendingItems.length === 1 ? "suggestion" : "suggestions"} will be discarded — the documents stay in the queue.`
            : `The ${pendingItems.length} undecided ${pendingItems.length === 1 ? "suggestion" : "suggestions"} and your edits will be discarded. The documents stay in the queue, nothing is changed in paperless-ngx.`
        }
        confirmLabel="Discard suggestions"
        onConfirm={() => {
          setShowDiscardConfirm(false);
          if (statsRef.current.docs > 0) {
            onFinished(statsRef.current.docs, statsRef.current.fields);
          } else {
            onDiscard();
          }
        }}
        onCancel={() => setShowDiscardConfirm(false)}
      />

      {focusIndex !== null && focusItems[focusIndex] && (
        <FocusReview
          items={focusItems}
          index={focusIndex}
          decisions={decisions}
          excludedMap={excludedMap}
          availableTags={availableTags}
          handlers={handlers}
          onToggleField={handleToggleField}
          onApply={handleApplyOne}
          onSkip={handleSkip}
          onNavigate={setFocusIndex}
          onClose={() => setFocusIndex(null)}
          applyingIds={applyingIds}
        />
      )}
    </section>
  );
};

export default SuggestionsReview;

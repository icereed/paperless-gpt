import React from "react";
import { DocumentSuggestion } from "../../DocumentProcessor";
import Button from "../ui/Button";
import Modal from "../ui/Modal";
import { FieldKey, changedFieldLabels } from "./fields";

interface ApplySummaryDialogProps {
  open: boolean;
  items: DocumentSuggestion[];
  excludedMap: Record<number, Set<FieldKey>>;
  filterTag: string | null;
  applying: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/** The moment of commitment: what will change, and that undo is one page away. */
const ApplySummaryDialog: React.FC<ApplySummaryDialogProps> = ({
  open,
  items,
  excludedMap,
  filterTag,
  applying,
  onConfirm,
  onCancel,
}) => {
  const perDoc = items.map((item) => ({
    item,
    labels: changedFieldLabels(item, excludedMap[item.id] || new Set()),
  }));
  const totalChanges = perDoc.reduce((sum, d) => sum + d.labels.length, 0);

  return (
    <Modal
      open={open}
      onClose={applying ? () => undefined : onCancel}
      title={`Apply suggestions to ${items.length} ${items.length === 1 ? "document" : "documents"}?`}
      size="lg"
    >
      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
        <p className="text-sm text-muted">
          {totalChanges} field {totalChanges === 1 ? "change" : "changes"} will
          be written to paperless-ngx
          {filterTag ? (
            <>
              , and the{" "}
              <span className="whitespace-nowrap rounded-full bg-primary-tint px-1.5 py-0.5 text-xs font-medium text-ink">
                {filterTag}
              </span>{" "}
              tag will be removed so the documents leave this queue.
            </>
          ) : (
            "."
          )}
        </p>

        <ul className="mt-4 space-y-2">
          {perDoc.map(({ item, labels }) => (
            <li
              key={item.id}
              className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-line pb-2 text-sm last:border-0"
            >
              <span
                className="min-w-0 flex-1 truncate font-medium"
                title={item.original_document.title}
              >
                {item.original_document.title}
              </span>
              <span className="text-xs text-muted">
                {labels.length > 0 ? labels.join(", ") : "queue tag only"}
              </span>
            </li>
          ))}
        </ul>

        <p className="mt-4 text-xs text-faint">
          Every change is recorded and can be reverted field by field on the
          History page.
        </p>
      </div>

      <div className="flex shrink-0 justify-end gap-2 border-t border-line px-6 py-4">
        <Button variant="secondary" onClick={onCancel} disabled={applying}>
          Cancel
        </Button>
        <Button variant="primary" onClick={onConfirm} loading={applying}>
          Apply to {items.length} {items.length === 1 ? "document" : "documents"}
        </Button>
      </div>
    </Modal>
  );
};

export default ApplySummaryDialog;

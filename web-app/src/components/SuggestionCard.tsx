import {
  ArrowsPointingOutIcon,
  CheckCircleIcon,
} from "@heroicons/react/24/outline";
import classNames from "classnames";
import React from "react";
import { DocumentSuggestion, TagOption } from "../DocumentProcessor";
import Button from "./ui/Button";
import {
  Decision,
  FieldKey,
  SuggestionEditHandlers,
  countChanges,
} from "./review/fields";
import SuggestionFields from "./review/SuggestionFields";

interface SuggestionCardProps {
  suggestion: DocumentSuggestion;
  availableTags: TagOption[];
  decision: Decision;
  excluded: Set<FieldKey>;
  onToggleField: (docId: number, key: FieldKey) => void;
  handlers: SuggestionEditHandlers;
  onApply: (docId: number) => void;
  onSkip: (docId: number) => void;
  onOpenFocus: (docId: number) => void;
  applying: boolean;
}

const SuggestionCard: React.FC<SuggestionCardProps> = ({
  suggestion,
  availableTags,
  decision,
  excluded,
  onToggleField,
  handlers,
  onApply,
  onSkip,
  onOpenFocus,
  applying,
}) => {
  const document = suggestion.original_document;
  const changeCount = countChanges(suggestion, excluded);
  const decided = decision !== "pending";

  return (
    <article
      aria-label={`Suggestions for “${document.title}”`}
      className={classNames(
        "flex h-full flex-col rounded-lg border border-line bg-surface shadow-card",
        decided && "opacity-70"
      )}
    >
      <header className="flex items-start justify-between gap-2 border-b border-line px-4 py-3">
        <div className="min-w-0">
          <h3 className="truncate text-sm font-medium" title={document.title}>
            {document.title}
          </h3>
          <p className="mt-0.5 text-xs text-faint">
            {decision === "applied" ? (
              <span className="inline-flex items-center gap-1 text-pos">
                <CheckCircleIcon className="h-3.5 w-3.5" aria-hidden="true" />
                Applied — revert anytime in History
              </span>
            ) : decision === "skipped" ? (
              "Skipped — stays in the queue"
            ) : changeCount === 0 ? (
              "No changes suggested"
            ) : (
              `${changeCount} field ${changeCount === 1 ? "change" : "changes"}`
            )}
          </p>
        </div>
        <button
          type="button"
          onClick={() => onOpenFocus(suggestion.id)}
          aria-label={`Open “${document.title}” in focus view`}
          title="Focus view (document text + scan)"
          className="rounded-md p-1.5 text-muted transition-colors duration-150 ease-out-quart hover:bg-surface-2 hover:text-ink"
        >
          <ArrowsPointingOutIcon className="h-4 w-4" aria-hidden="true" />
        </button>
      </header>

      <div className="flex-1 px-4 py-1.5">
        <SuggestionFields
          suggestion={suggestion}
          availableTags={availableTags}
          excluded={excluded}
          onToggleField={onToggleField}
          handlers={handlers}
          disabled={decided || applying}
        />
      </div>

      {!decided && (
        <footer className="flex items-center justify-end gap-2 border-t border-line px-4 py-3">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onSkip(suggestion.id)}
            disabled={applying}
          >
            Skip
          </Button>
          <Button
            size="sm"
            variant="primary"
            onClick={() => onApply(suggestion.id)}
            loading={applying}
          >
            Apply
          </Button>
        </footer>
      )}
    </article>
  );
};

export default SuggestionCard;

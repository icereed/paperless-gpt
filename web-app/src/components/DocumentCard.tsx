import { ChevronDownIcon } from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useState } from "react";
import { Document } from "../DocumentProcessor";

interface DocumentCardProps {
  document: Document;
  isSelected?: boolean;
  onSelect?: (documentId: number) => void;
}

const DocumentCard: React.FC<DocumentCardProps> = ({
  document,
  isSelected,
  onSelect,
}) => {
  const [expanded, setExpanded] = useState(false);
  const contentId = `doc-content-${document.id}`;

  return (
    <article
      className={classNames(
        // "document-card" is a stable hook for E2E tests; keep it when restyling.
        "document-card flex flex-col rounded-lg border bg-surface p-4 shadow-card transition-colors duration-150 ease-out-quart",
        isSelected ? "border-primary" : "border-line"
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <h3 className="min-w-0 text-base font-medium leading-snug">
          {document.title}
        </h3>
        {onSelect && (
          <input
            type="checkbox"
            checked={!!isSelected}
            onChange={() => onSelect(document.id)}
            aria-label={`Select “${document.title}”`}
            className="mt-0.5 h-5 w-5 shrink-0 cursor-pointer rounded accent-primary"
          />
        )}
      </div>

      {(document.correspondent || document.document_type_name) && (
        <p className="mt-1 truncate text-sm text-muted">
          {[document.correspondent, document.document_type_name]
            .filter(Boolean)
            .join(" · ")}
        </p>
      )}

      {document.tags.length > 0 && (
        <ul className="mt-3 flex flex-wrap gap-1.5" aria-label="Tags">
          {document.tags.map((tag) => (
            <li
              key={tag}
              className="rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium"
            >
              {tag}
            </li>
          ))}
        </ul>
      )}

      {document.content && (
        <div className="mt-3 border-t border-line pt-3">
          <p
            className={classNames(
              "whitespace-pre-wrap text-sm text-muted",
              expanded ? "max-h-56 overflow-y-auto" : "line-clamp-2"
            )}
            id={contentId}
          >
            {document.content}
          </p>
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            aria-expanded={expanded}
            aria-controls={contentId}
            className="mt-2 inline-flex items-center gap-1 text-xs font-medium text-muted hover:text-ink"
          >
            <ChevronDownIcon
              className={classNames(
                "h-3.5 w-3.5 transition-transform duration-150 ease-out-quart",
                expanded && "rotate-180"
              )}
              aria-hidden="true"
            />
            {expanded ? "Hide document text" : "Show document text"}
          </button>
        </div>
      )}
    </article>
  );
};

export default DocumentCard;

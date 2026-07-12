import {
  Combobox,
  ComboboxInput,
  ComboboxOption,
  ComboboxOptions,
} from "@headlessui/react";
import { DocumentIcon, MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useEffect, useRef, useState } from "react";
import { Document } from "../../DocumentProcessor";
import { searchDocuments } from "./api";

interface DocumentPickerProps {
  selected: Document | null;
  onSelect: (doc: Document) => void;
  disabled?: boolean;
}

/**
 * The Playground's front door: search any paperless-ngx document by title or
 * content, browse the most recently added ones (empty query), or paste a
 * document ID / paperless URL.
 */
const DocumentPicker: React.FC<DocumentPickerProps> = ({
  selected,
  onSelect,
  disabled,
}) => {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Document[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => {
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(async () => {
      setLoading(true);
      setError(null);
      try {
        setResults(await searchDocuments(query));
      } catch (err) {
        console.error("Document search failed:", err);
        setError("Search failed — check the backend connection.");
      } finally {
        setLoading(false);
      }
    }, query ? 250 : 0);
    return () => clearTimeout(debounceRef.current);
  }, [query]);

  return (
    <Combobox<Document | null>
      value={selected}
      onChange={(doc) => doc && onSelect(doc)}
      disabled={disabled}
      immediate
    >
      <div className="relative">
        <MagnifyingGlassIcon
          className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-faint"
          aria-hidden="true"
        />
        <ComboboxInput
          aria-label="Search documents"
          placeholder="Search by title or content, paste an ID or paperless URL…"
          displayValue={(doc: Document | null) => doc?.title || ""}
          onChange={(e) => setQuery(e.target.value)}
          className="h-10 w-full rounded-md border border-line bg-surface pl-9 pr-3 text-sm text-ink placeholder:text-faint"
        />
        <ComboboxOptions
          transition
          className="absolute z-dropdown mt-1 max-h-96 w-full overflow-y-auto rounded-md border border-line bg-surface p-1 shadow-raised duration-100 ease-out-quart data-[closed]:opacity-0 empty:invisible"
        >
          {error && <p className="px-3 py-2 text-sm text-neg">{error}</p>}
          {!error && !loading && results.length === 0 && (
            <p className="px-3 py-2 text-sm text-muted">
              No documents found{query ? ` for “${query}”` : ""}.
            </p>
          )}
          {!query && results.length > 0 && (
            <p className="px-3 pb-1 pt-2 text-xs font-medium text-faint">
              Recently added
            </p>
          )}
          {results.map((doc) => (
            <ComboboxOption
              key={doc.id}
              value={doc}
              className={classNames(
                "flex cursor-pointer items-center gap-3 rounded px-2 py-2",
                "data-[focus]:bg-primary-tint"
              )}
            >
              <img
                src={`./api/documents/${doc.id}/thumb`}
                alt=""
                loading="lazy"
                className="h-12 w-9 shrink-0 rounded border border-line bg-surface-2 object-cover"
                onError={(e) => {
                  (e.target as HTMLImageElement).style.visibility = "hidden";
                }}
              />
              <span className="min-w-0">
                <span className="block truncate text-sm font-medium">
                  {doc.title}
                </span>
                <span className="block truncate text-xs text-muted">
                  {[
                    `#${doc.id}`,
                    doc.correspondent,
                    doc.created_date,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              </span>
            </ComboboxOption>
          ))}
        </ComboboxOptions>
      </div>
      {selected && (
        <div className="mt-3 flex items-center gap-3 rounded-md border border-line bg-surface-2 px-3 py-2">
          <DocumentIcon className="h-4 w-4 shrink-0 text-muted" aria-hidden="true" />
          <p className="min-w-0 flex-1 truncate text-sm">
            <span className="font-medium">{selected.title}</span>
            <span className="text-faint"> · #{selected.id}</span>
          </p>
          <span
            className={classNames(
              "shrink-0 rounded-full px-2 py-0.5 text-xs font-medium",
              selected.content
                ? "bg-surface text-muted"
                : "bg-warn-tint text-warn"
            )}
          >
            {selected.content
              ? `${selected.content.length.toLocaleString()} chars of text`
              : "no text yet"}
          </span>
        </div>
      )}
    </Combobox>
  );
};

export default DocumentPicker;

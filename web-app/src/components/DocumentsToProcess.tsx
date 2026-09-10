import React, { useEffect, useRef } from "react";
import { Document } from "../DocumentProcessor";
import DocumentCard from "./DocumentCard";

export interface DocumentsToProcessProps {
  documents: Document[];
  selectedDocuments?: number[];
  onSelectDocument?: (documentId: number) => void;
  /** When provided, renders a select-all toolbar above the grid. */
  onToggleAll?: () => void;
  children?: React.ReactNode;
}

const DocumentsToProcess: React.FC<DocumentsToProcessProps> = ({
  documents,
  selectedDocuments,
  onSelectDocument,
  onToggleAll,
  children,
}) => {
  const selectedCount = selectedDocuments?.length ?? 0;
  const allSelected = selectedCount === documents.length && documents.length > 0;
  const selectAllRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (selectAllRef.current) {
      selectAllRef.current.indeterminate =
        selectedCount > 0 && selectedCount < documents.length;
    }
  }, [selectedCount, documents.length]);

  return (
    <section>
      {children}
      {onToggleAll && (
        <div className="mb-3 flex items-center justify-between">
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-muted">
            <input
              ref={selectAllRef}
              type="checkbox"
              checked={allSelected}
              onChange={onToggleAll}
              className="h-4 w-4 cursor-pointer rounded accent-primary"
            />
            {allSelected
              ? "All documents selected"
              : selectedCount === 0
                ? "Select all documents"
                : `${selectedCount} of ${documents.length} selected`}
          </label>
        </div>
      )}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {documents.map((doc) => (
          <DocumentCard
            key={doc.id}
            document={doc}
            isSelected={selectedDocuments?.includes(doc.id)}
            onSelect={onSelectDocument}
          />
        ))}
      </div>
    </section>
  );
};

export default DocumentsToProcess;

import React from "react";
import { Document, SuggestionJobStatus } from "../DocumentProcessor";
import Button from "./ui/Button";

interface JobProgressProps {
  job: SuggestionJobStatus;
  documents: Document[];
  onCancel: () => void;
}

/** Live progress for an async suggestion job: n of m, current document, cancel. */
const JobProgress: React.FC<JobProgressProps> = ({
  job,
  documents,
  onCancel,
}) => {
  const total = Math.max(job.total_documents, 1);
  const done = Math.min(job.documents_done, total);
  const fraction = done / total;
  const currentDoc =
    job.current_document_id > 0
      ? documents.find((doc) => doc.id === job.current_document_id)
      : undefined;

  return (
    <div className="mt-6 rounded-lg border border-line bg-surface p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-medium" role="status">
            Generating suggestions — {done} of {job.total_documents}{" "}
            {job.total_documents === 1 ? "document" : "documents"}
          </p>
          <p className="mt-0.5 truncate text-sm text-muted">
            {job.status === "pending"
              ? "Waiting for a worker…"
              : currentDoc
                ? `Working on “${currentDoc.title}”`
                : "Working…"}
          </p>
        </div>
        <Button variant="danger" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </div>

      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={job.total_documents}
        aria-valuenow={done}
        aria-label="Documents processed"
        className="mt-4 h-1.5 overflow-hidden rounded-full bg-surface-2"
      >
        <div
          className="h-full origin-left rounded-full bg-primary transition-transform duration-300 ease-out-quart"
          style={{ transform: `scaleX(${fraction})` }}
        />
      </div>

      <p className="mt-3 text-xs text-faint">
        Generation runs on the server — you can leave this page and come back.
      </p>
    </div>
  );
};

export default JobProgress;

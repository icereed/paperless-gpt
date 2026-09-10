import { ArrowPathIcon, InboxIcon } from "@heroicons/react/24/outline";
import React from "react";
import Button from "./ui/Button";

interface NoDocumentsProps {
  filterTag: string | null;
  onReload: () => void;
  reloading: boolean;
}

/** Empty state that teaches how documents get into the queue. */
const NoDocuments: React.FC<NoDocumentsProps> = ({
  filterTag,
  onReload,
  reloading,
}) => (
  <div className="mx-auto mt-16 max-w-xl rounded-lg border border-line bg-surface p-8 text-center">
    <InboxIcon className="mx-auto h-10 w-10 text-faint" aria-hidden="true" />
    <h1 className="mt-4 text-lg font-semibold">No documents waiting</h1>
    <p className="mt-2 text-sm text-muted">
      paperless-gpt picks up every document tagged{" "}
      {filterTag ? (
        <span className="whitespace-nowrap rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium text-ink">
          {filterTag}
        </span>
      ) : (
        "with the configured filter tag"
      )}{" "}
      in paperless-ngx. Tag a document there and it appears here — this page
      checks automatically every few seconds.
    </p>
    <div className="mt-6 flex items-center justify-center gap-3">
      <Button variant="secondary" onClick={onReload} loading={reloading}>
        {!reloading && <ArrowPathIcon className="h-4 w-4" aria-hidden="true" />}
        Check now
      </Button>
      <a
        href="https://github.com/icereed/paperless-gpt#how-it-works"
        target="_blank"
        rel="noopener noreferrer"
        className="text-sm font-medium text-primary hover:underline"
      >
        How tagging works
      </a>
    </div>
  </div>
);

export default NoDocuments;

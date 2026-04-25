import React, { useState } from "react";
import ArrowTopRightOnSquareIcon from "@heroicons/react/24/outline/ArrowTopRightOnSquareIcon";
import EyeIcon from "@heroicons/react/24/outline/EyeIcon";
import ArrowPathIcon from "@heroicons/react/24/outline/ArrowPathIcon";
import TrashIcon from "@heroicons/react/24/outline/TrashIcon";
import { Document } from "../DocumentProcessor";
import DocumentPreviewModal from "./DocumentPreviewModal";

interface DocumentCardProps {
  document: Document;
  isSelected?: boolean;
  onSelect?: (documentId: number) => void;
  paperlessUrl?: string;
  onDelete?: (documentId: number) => void;
  onReprocess?: (documentId: number) => void;
}

const DocumentCard: React.FC<DocumentCardProps> = ({
  document,
  isSelected,
  onSelect,
  paperlessUrl,
  onDelete,
  onReprocess,
}) => {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);

  const handleDeleteClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (confirmDelete) {
      if (onDelete) {
        onDelete(document.id);
      }
    } else {
      setConfirmDelete(true);
    }
  };

  const handleCancelDelete = (e: React.MouseEvent) => {
    e.stopPropagation();
    setConfirmDelete(false);
  };

  const handlePreviewClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    setIsPreviewOpen(true);
  };

  return (
    <>
      <div
        className={`document-card relative overflow-hidden rounded-2xl border border-gray-200 bg-white p-4 shadow-sm transition hover:-translate-y-0.5 hover:shadow-lg dark:border-gray-700 dark:bg-gray-800 ${isSelected ? "ring-2 ring-blue-500" : ""} ${onSelect ? "cursor-pointer" : ""}`}
        onClick={() => onSelect && onSelect(document.id)}
      >
        {onSelect && (
          <input
            type="checkbox"
            checked={isSelected}
            onChange={() => onSelect(document.id)}
            onClick={(e) => e.stopPropagation()}
            className="absolute right-4 top-4 h-5 w-5 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
          />
        )}

        <div
          className="mb-4 flex flex-wrap items-center gap-2 pr-8"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            onClick={handlePreviewClick}
            className="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-700 transition-colors hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-100 dark:hover:bg-gray-600"
          >
            <EyeIcon className="h-3.5 w-3.5" />
            Preview
          </button>
          {paperlessUrl && (
            <a
              href={`${paperlessUrl}/documents/${document.id}/details`}
              target="_blank"
              rel="noopener noreferrer"
              title="View in Paperless-ngx"
              className="inline-flex items-center gap-1 rounded-md bg-blue-100 px-2.5 py-1 text-xs font-medium text-blue-700 transition-colors hover:bg-blue-200 dark:bg-blue-900 dark:text-blue-300 dark:hover:bg-blue-800"
            >
              <ArrowTopRightOnSquareIcon className="h-3.5 w-3.5" />
              View
            </a>
          )}
          {onReprocess && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onReprocess(document.id);
              }}
              className="inline-flex items-center gap-1 rounded-md bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-700 transition-colors hover:bg-amber-200 dark:bg-amber-900 dark:text-amber-200 dark:hover:bg-amber-800"
            >
              <ArrowPathIcon className="h-3.5 w-3.5" />
              Reprocess
            </button>
          )}
          {onDelete &&
            (confirmDelete ? (
              <>
                <button
                  onClick={handleDeleteClick}
                  title="Confirm delete"
                  className="inline-flex items-center gap-1 rounded-md bg-red-600 px-2.5 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700"
                >
                  <TrashIcon className="h-3.5 w-3.5" />
                  Sure?
                </button>
                <button
                  onClick={handleCancelDelete}
                  title="Cancel"
                  className="rounded-md bg-gray-200 px-2.5 py-1 text-xs font-medium text-gray-700 transition-colors hover:bg-gray-300 dark:bg-gray-600 dark:text-gray-200 dark:hover:bg-gray-500"
                >
                  Cancel
                </button>
              </>
            ) : (
              <button
                onClick={handleDeleteClick}
                title="Delete document"
                className="inline-flex items-center gap-1 rounded-md bg-red-100 px-2.5 py-1 text-xs font-medium text-red-700 transition-colors hover:bg-red-200 dark:bg-red-900 dark:text-red-300 dark:hover:bg-red-800"
              >
                <TrashIcon className="h-3.5 w-3.5" />
                Delete
              </button>
            ))}
        </div>

        <div className="space-y-3">
          <div>
            <h3 className="pr-4 text-lg font-semibold text-gray-900 dark:text-gray-100">
              {document.title}
            </h3>
            <p className="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
              {document.content.length > 180
                ? `${document.content.substring(0, 180)}...`
                : document.content || "No OCR text available yet."}
            </p>
          </div>
          <p className="text-sm text-gray-600 dark:text-gray-300">
            Correspondent:{" "}
            <span className="font-semibold text-blue-600 dark:text-blue-400">
              {document.correspondent || "Unknown"}
            </span>
          </p>
          <div className="flex flex-wrap gap-2">
            {document.tags.length > 0 ? (
              document.tags.map((tag) => (
                <span
                  key={tag}
                  className="rounded-full bg-blue-50 px-2.5 py-1 text-xs font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-200"
                >
                  {tag}
                </span>
              ))
            ) : (
              <span className="text-xs text-gray-500 dark:text-gray-400">
                No tags yet
              </span>
            )}
          </div>
        </div>
      </div>

      <DocumentPreviewModal
        isOpen={isPreviewOpen}
        onClose={() => setIsPreviewOpen(false)}
        title={document.title}
        content={document.content}
        correspondent={document.correspondent}
        tags={document.tags}
      />
    </>
  );
};

export default DocumentCard;

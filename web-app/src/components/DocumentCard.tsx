import React from "react";
import { Document } from "../DocumentProcessor";

interface DocumentCardProps {
  document: Document;
}

const DocumentCard: React.FC<DocumentCardProps> = ({ document }) => (
  <div className="document-card bg-white dark:bg-gray-800 shadow-lg shadow-blue-500/50 rounded-md p-4 relative group overflow-hidden">
    <h3 className="text-lg font-semibold text-gray-800 dark:text-gray-200">{document.title}</h3>
    <p className="text-sm text-gray-600 dark:text-gray-400 mt-2 truncate">
      {document.content.length > 100
        ? `${document.content.substring(0, 100)}...`
        : document.content}
    </p>
    <p className="text-sm text-gray-600 dark:text-gray-400 mt-2">
      Correspondent: <span className="font-bold text-blue-600 dark:text-blue-400">{document.correspondent}</span>
    </p>
    <div className="mt-4">
      {document.tags.map((tag) => (
        <span
          key={tag}
          className="bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 text-xs font-medium mr-2 px-2.5 py-0.5 rounded-full"
        >
          {tag}
        </span>
      ))}
    </div>
    <div className="absolute inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 opacity-0 group-hover:opacity-100 transition-opacity duration-300 flex items-center justify-center p-4 rounded-md">
      <div className="text-sm text-white p-2 bg-gray-800 dark:bg-gray-900 rounded-md w-full max-h-full overflow-y-auto">
        <h3 className="text-lg font-semibold text-white">{document.title}</h3>
        <p className="mt-2 whitespace-pre-wrap">{document.content}</p>
        <p className="mt-2">
          Correspondent: <span className="font-bold text-blue-400">{document.correspondent}</span>
        </p>
        <div className="mt-4">
          {document.tags.map((tag) => (
            <span
              key={tag}
              className="bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 text-xs font-medium mr-2 px-2.5 py-0.5 rounded-full"
            >
              {tag}
            </span>
          ))}
        </div>
      </div>
    </div>
  </div>
);

export default DocumentCard;
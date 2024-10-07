import ArrowPathIcon from "@heroicons/react/24/outline/ArrowPathIcon";
import React from "react";
import { Document } from "../DocumentProcessor";
import DocumentCard from "./DocumentCard";

interface DocumentsToProcessProps {
  documents: Document[];
  generateTitles: boolean;
  setGenerateTitles: React.Dispatch<React.SetStateAction<boolean>>;
  generateTags: boolean;
  setGenerateTags: React.Dispatch<React.SetStateAction<boolean>>;
  onProcess: () => void;
  processing: boolean;
  onReload: () => void;
}

const DocumentsToProcess: React.FC<DocumentsToProcessProps> = ({
  documents,
  generateTitles,
  setGenerateTitles,
  generateTags,
  setGenerateTags,
  onProcess,
  processing,
  onReload,
}) => (
  <section>
    <div className="flex justify-between items-center mb-6">
      <h2 className="text-2xl font-semibold text-gray-700">Documents to Process</h2>
      <div className="flex space-x-2">
        <button
          onClick={onReload}
          disabled={processing}
          className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 focus:outline-none"
        >
          <ArrowPathIcon className="h-5 w-5" />
        </button>
        <button
          onClick={onProcess}
          disabled={processing}
          className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 focus:outline-none"
        >
          {processing ? "Processing..." : "Generate Suggestions"}
        </button>
      </div>
    </div>

    <div className="flex space-x-4 mb-6">
      <label className="flex items-center space-x-2">
        <input
          type="checkbox"
          checked={generateTitles}
          onChange={(e) => setGenerateTitles(e.target.checked)}
        />
        <span>Generate Titles</span>
      </label>
      <label className="flex items-center space-x-2">
        <input
          type="checkbox"
          checked={generateTags}
          onChange={(e) => setGenerateTags(e.target.checked)}
        />
        <span>Generate Tags</span>
      </label>
    </div>

    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      {documents.map((doc) => (
        <DocumentCard key={doc.id} document={doc} />
      ))}
    </div>
  </section>
);

export default DocumentsToProcess;
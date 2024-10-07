import { ArrowPathIcon } from "@heroicons/react/24/outline";
import React from "react";

interface NoDocumentsProps {
  filterTag: string | null;
  onReload: () => void;
  processing: boolean;
}

const NoDocuments: React.FC<NoDocumentsProps> = ({
  filterTag,
  onReload,
  processing,
}) => (
  <div className="flex flex-col items-center justify-center min-h-screen">
    <p className="text-xl font-semibold mb-4">
      No documents found with filter tag{" "}
      {filterTag && (
        <span className="bg-blue-100 text-blue-800 text-sm font-medium px-2.5 py-0.5 rounded-full">
          {filterTag}
        </span>
      )}
      .
    </p>
    <button
      onClick={onReload}
      disabled={processing}
      className="flex items-center bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 focus:outline-none"
    >
      Reload
      <ArrowPathIcon className="h-5 w-5 ml-2" />
    </button>
  </div>
);

export default NoDocuments;
import React from "react";
import { DocumentSuggestion, TagOption } from "../DocumentProcessor";
import SuggestionCard from "./SuggestionCard";

interface SuggestionsReviewProps {
  suggestions: DocumentSuggestion[];
  availableTags: TagOption[];
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: TagOption) => void;
  onTagDeletion: (docId: number, index: number) => void;
  onCorrespondentChange: (docId: number, correspondent: string) => void;
  onBack: () => void;
  onUpdate: () => void;
  updating: boolean;
}

const SuggestionsReview: React.FC<SuggestionsReviewProps> = ({
  suggestions,
  availableTags,
  onTitleChange,
  onTagAddition,
  onTagDeletion,
  onCorrespondentChange,
  onBack,
  onUpdate,
  updating,
}) => (
  <section className="suggestions-review">
    <h2 className="text-2xl font-semibold text-gray-700 dark:text-gray-200 mb-6">
      Review and Edit Suggested Titles
    </h2>
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      {suggestions.map((doc) => (
        <SuggestionCard
          key={doc.id}
          suggestion={doc}
          availableTags={availableTags}
          onTitleChange={onTitleChange}
          onTagAddition={onTagAddition}
          onTagDeletion={onTagDeletion}
          onCorrespondentChange={onCorrespondentChange}
        />
      ))}
    </div>
    <div className="flex justify-end space-x-4 mt-6">
      <button
        onClick={onBack}
        className="bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-200 px-4 py-2 rounded hover:bg-gray-300 dark:hover:bg-gray-600 focus:outline-none"
      >
        Back
      </button>
      <button
        onClick={onUpdate}
        disabled={updating}
        className={`${
          updating
            ? "bg-green-400 dark:bg-green-600 cursor-not-allowed"
            : "bg-green-600 dark:bg-green-700 hover:bg-green-700 dark:hover:bg-green-800"
        } text-white px-4 py-2 rounded focus:outline-none`}
      >
        {updating ? "Updating..." : "Apply Suggestions"}
      </button>
    </div>
  </section>
);

export default SuggestionsReview;
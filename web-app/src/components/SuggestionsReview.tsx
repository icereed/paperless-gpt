import React from "react";
import { DocumentSuggestion, TagOption } from "../DocumentProcessor";
import SuggestionCard from "./SuggestionCard";

interface SuggestionsReviewProps {
  suggestions: DocumentSuggestion[];
  availableTags: TagOption[];
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: TagOption) => void;
  onTagDeletion: (docId: number, index: number) => void;
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
  onBack,
  onUpdate,
  updating,
}) => (
  <section>
    <h2 className="text-2xl font-semibold text-gray-700 mb-6">
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
        />
      ))}
    </div>
    <div className="flex justify-end space-x-4 mt-6">
      <button
        onClick={onBack}
        className="bg-gray-200 text-gray-700 px-4 py-2 rounded hover:bg-gray-300 focus:outline-none"
      >
        Back
      </button>
      <button
        onClick={onUpdate}
        disabled={updating}
        className={`${
          updating
            ? "bg-green-400 cursor-not-allowed"
            : "bg-green-600 hover:bg-green-700"
        } text-white px-4 py-2 rounded focus:outline-none`}
      >
        {updating ? "Updating..." : "Apply Suggestions"}
      </button>
    </div>
  </section>
);

export default SuggestionsReview;
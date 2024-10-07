import React from "react";
import { InformationCircleIcon } from "@heroicons/react/24/outline";
import { ReactTags } from "react-tag-autocomplete";
import { DocumentSuggestion, TagOption } from "../DocumentProcessor";

interface SuggestionCardProps {
  suggestion: DocumentSuggestion;
  availableTags: TagOption[];
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: TagOption) => void;
  onTagDeletion: (docId: number, index: number) => void;
}

const SuggestionCard: React.FC<SuggestionCardProps> = ({
  suggestion,
  availableTags,
  onTitleChange,
  onTagAddition,
  onTagDeletion,
}) => (
  <div className="bg-white shadow rounded-md p-4 relative group">
    <div className="flex items-center">
      <h3 className="text-lg font-semibold text-gray-800">
        {suggestion.original_document.title}
      </h3>
      <InformationCircleIcon
        className="h-6 w-6 text-gray-500 ml-2 cursor-pointer"
        title={suggestion.original_document.content}
      />
    </div>
    <input
      type="text"
      value={suggestion.suggested_title || ""}
      onChange={(e) => onTitleChange(suggestion.id, e.target.value)}
      className="w-full border border-gray-300 rounded px-2 py-1 mt-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
    />
    <div className="mt-4">
      <ReactTags
        selected={
          suggestion.suggested_tags?.map((tag, index) => ({
            id: index.toString(),
            name: tag,
            label: tag,
            value: index.toString(),
          })) || []
        }
        suggestions={availableTags.map(tag => ({ id: tag.id, name: tag.name, label: tag.name, value: tag.id }))}
        onAdd={(tag) => onTagAddition(suggestion.id, { id: String(tag.label), name: String(tag.value) })}
        onDelete={(index) => onTagDeletion(suggestion.id, index)}
        allowNew={true}
        placeholderText="Add a tag"
      />
    </div>
  </div>
);

export default SuggestionCard;
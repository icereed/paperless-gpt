import React from "react";
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
}) => {
  const document = suggestion.original_document;
  return (
    <div className="bg-white shadow shadow-blue-500/50 rounded-md p-4 relative flex flex-col justify-between h-full">
      <div className="flex items-center group relative">
        <div className="relative">
          <h3 className="text-lg font-semibold text-gray-800">
            {document.title}
          </h3>
          <p className="text-sm text-gray-600 mt-2 truncate">
            {document.content.length > 40
              ? `${document.content.substring(0, 40)}...`
              : document.content}
          </p>
          <div className="mt-4">
            {document.tags.map((tag) => (
              <span
                key={tag}
                className="bg-blue-100 text-blue-800 text-xs font-medium mr-2 px-2.5 py-0.5 rounded-full"
              >
                {tag}
              </span>
            ))}
          </div>
        </div>
        <div className="absolute inset-0 bg-black bg-opacity-50 opacity-0 group-hover:opacity-100 transition-opacity duration-300 flex items-center justify-center p-4 rounded-md">
          <div className="text-sm text-white p-2 bg-gray-800 rounded-md w-full max-h-full overflow-y-auto">
            <p className="mt-2 whitespace-pre-wrap">{document.content}</p>
          </div>
        </div>
      </div>
      <div className="mt-4">
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
            suggestions={availableTags.map((tag) => ({
              id: tag.id,
              name: tag.name,
              label: tag.name,
              value: tag.id,
            }))}
            onAdd={(tag) =>
              onTagAddition(suggestion.id, {
                id: String(tag.label),
                name: String(tag.value),
              })
            }
            onDelete={(index) => onTagDeletion(suggestion.id, index)}
            allowNew={true}
            placeholderText="Add a tag"
          />
        </div>
      </div>
    </div>
  );
};

export default SuggestionCard;
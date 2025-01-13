import React from "react";
import { ReactTags } from "react-tag-autocomplete";
import { DocumentSuggestion, TagOption } from "../DocumentProcessor";

interface SuggestionCardProps {
  suggestion: DocumentSuggestion;
  availableTags: TagOption[];
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: TagOption) => void;
  onTagDeletion: (docId: number, index: number) => void;
  onCorrespondentChange: (docId: number, correspondent: string) => void;
}

const SuggestionCard: React.FC<SuggestionCardProps> = ({
  suggestion,
  availableTags,
  onTitleChange,
  onTagAddition,
  onTagDeletion,
  onCorrespondentChange,
}) => {
  const sortedAvailableTags = availableTags.sort((a, b) => a.name.localeCompare(b.name));
  const document = suggestion.original_document;
  return (
    <div className="bg-white dark:bg-gray-800 shadow-lg shadow-blue-500/50 rounded-md p-4 relative flex flex-col justify-between h-full">
      <div className="flex items-center group relative">
        <div className="relative">
          <h3 className="text-lg font-semibold text-gray-800 dark:text-gray-200">
            {document.title}
          </h3>
          <p className="text-sm text-gray-600 dark:text-gray-400 mt-2 truncate">
            {document.content.length > 40
              ? `${document.content.substring(0, 40)}...`
              : document.content}
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
        <div className="absolute inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 opacity-0 group-hover:opacity-100 transition-opacity duration-300 flex items-center justify-center p-4 rounded-md">
          <div className="text-sm text-white p-2 bg-gray-800 dark:bg-gray-900 rounded-md w-full max-h-full overflow-y-auto">
            <p className="mt-2 whitespace-pre-wrap">{document.content}</p>
          </div>
        </div>
      </div>
      <div className="mt-4">
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
          Suggested Title
        </label>
        <input
          type="text"
          value={suggestion.suggested_title || ""}
          onChange={(e) => onTitleChange(suggestion.id, e.target.value)}
          className="w-full border border-gray-300 dark:border-gray-600 rounded px-2 py-1 mt-2 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-gray-200"
        />
        <div className="mt-4">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
            Suggested Tags
          </label>
          <ReactTags
            selected={
              suggestion.suggested_tags?.map((tag, index) => ({
                id: index.toString(),
                name: tag,
                label: tag,
                value: index.toString(),
              })) || []
            }
            suggestions={sortedAvailableTags.map((tag) => ({
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
            classNames={{
              root: "react-tags dark:bg-gray-800",
              rootIsActive: "is-active",
              rootIsDisabled: "is-disabled",
              rootIsInvalid: "is-invalid",
              label: "react-tags__label",
              tagList: "react-tags__list",
              tagListItem: "react-tags__list-item",
              tag: "react-tags__tag dark:bg-blue-900 dark:text-blue-200",
              tagName: "react-tags__tag-name",
              comboBox: "react-tags__combobox dark:bg-gray-700 dark:text-gray-200",
              input: "react-tags__combobox-input dark:bg-gray-700 dark:text-gray-200",
              listBox: "react-tags__listbox dark:bg-gray-700 dark:text-gray-200",
              option: "react-tags__listbox-option dark:bg-gray-700 dark:text-gray-200 hover:bg-blue-500 dark:hover:bg-blue-800",
              optionIsActive: "is-active",
              highlight: "react-tags__highlight dark:bg-gray-800",
            }}
          />
        </div>
        <div className="mt-4">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
            Suggested Correspondent
          </label>
          <input
            type="text"
            value={suggestion.suggested_correspondent || ""}
            onChange={(e) => onCorrespondentChange(suggestion.id, e.target.value)}
            className="w-full border border-gray-300 dark:border-gray-600 rounded px-2 py-1 mt-2 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-gray-200"
            placeholder="Correspondent"
          />
        </div>
      </div>
    </div>
  );
};

export default SuggestionCard;
import classNames from "classnames";
import React from "react";
import { ReactTags } from "react-tag-autocomplete";
import { DocumentSuggestion, TagOption } from "../../DocumentProcessor";
import {
  FieldKey,
  SuggestionEditHandlers,
  fieldChanged,
  fieldLabels,
  originalValue,
} from "./fields";

interface FieldRowProps {
  suggestion: DocumentSuggestion;
  fieldKey: FieldKey;
  excluded: boolean;
  onToggleField: (docId: number, key: FieldKey) => void;
  children: React.ReactNode;
}

/**
 * One field as a diff: original value (when it changes) above the editable
 * suggested value, with a per-field apply toggle.
 */
const FieldRow: React.FC<FieldRowProps> = ({
  suggestion,
  fieldKey,
  excluded,
  onToggleField,
  children,
}) => {
  const changed = fieldChanged(suggestion, fieldKey);
  const original = originalValue(suggestion, fieldKey);
  const originalText = Array.isArray(original)
    ? original.join(", ")
    : original;
  const label = fieldLabels[fieldKey];

  return (
    <div className="py-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-muted">{label}</span>
        {changed && (
          <label className="inline-flex cursor-pointer items-center gap-1.5 text-xs text-muted">
            <input
              type="checkbox"
              checked={!excluded}
              onChange={() => onToggleField(suggestion.id, fieldKey)}
              aria-label={`Apply suggested ${label.toLowerCase()}`}
              className="h-3.5 w-3.5 cursor-pointer rounded accent-primary"
            />
            Apply
          </label>
        )}
      </div>

      {changed && originalText && (
        <p className="mt-1 flex items-baseline gap-1.5 text-sm">
          <span className="select-none text-neg" aria-hidden="true">
            −
          </span>
          <span className="min-w-0 break-words text-muted line-through">
            <span className="sr-only">Current value: </span>
            {originalText}
          </span>
        </p>
      )}

      <div
        className={classNames(
          "mt-1 flex items-start gap-1.5",
          excluded && "opacity-50"
        )}
      >
        {changed && (
          <span
            className="select-none pt-1.5 text-sm text-pos"
            aria-hidden="true"
          >
            +
          </span>
        )}
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  );
};

const inputClasses =
  "w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink disabled:cursor-not-allowed";

interface SuggestionFieldsProps {
  suggestion: DocumentSuggestion;
  availableTags: TagOption[];
  excluded: Set<FieldKey>;
  onToggleField: (docId: number, key: FieldKey) => void;
  handlers: SuggestionEditHandlers;
  disabled?: boolean;
}

/** The editable field list of one suggestion, shared by card and focus view. */
const SuggestionFields: React.FC<SuggestionFieldsProps> = ({
  suggestion,
  availableTags,
  excluded,
  onToggleField,
  handlers,
  disabled,
}) => {
  const sortedAvailableTags = [...availableTags].sort((a, b) =>
    a.name.localeCompare(b.name)
  );

  return (
    <div className="divide-y divide-line">
      <FieldRow
        suggestion={suggestion}
        fieldKey="title"
        excluded={excluded.has("title")}
        onToggleField={onToggleField}
      >
        <input
          type="text"
          value={suggestion.suggested_title || ""}
          onChange={(e) => handlers.onTitleChange(suggestion.id, e.target.value)}
          disabled={disabled || excluded.has("title")}
          aria-label="Suggested title"
          className={inputClasses}
        />
      </FieldRow>

      <FieldRow
        suggestion={suggestion}
        fieldKey="tags"
        excluded={excluded.has("tags")}
        onToggleField={onToggleField}
      >
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
            handlers.onTagAddition(suggestion.id, {
              id: String(tag.value ?? tag.label),
              name: String(tag.label),
            })
          }
          onDelete={(index) => handlers.onTagDeletion(suggestion.id, index)}
          allowNew={true}
          placeholderText="Add a tag"
          labelText="Suggested tags"
          isDisabled={disabled || excluded.has("tags")}
        />
      </FieldRow>

      <FieldRow
        suggestion={suggestion}
        fieldKey="correspondent"
        excluded={excluded.has("correspondent")}
        onToggleField={onToggleField}
      >
        <input
          type="text"
          value={suggestion.suggested_correspondent || ""}
          onChange={(e) =>
            handlers.onCorrespondentChange(suggestion.id, e.target.value)
          }
          disabled={disabled || excluded.has("correspondent")}
          aria-label="Suggested correspondent"
          placeholder="Correspondent"
          className={inputClasses}
        />
      </FieldRow>

      <FieldRow
        suggestion={suggestion}
        fieldKey="document_type"
        excluded={excluded.has("document_type")}
        onToggleField={onToggleField}
      >
        <input
          type="text"
          value={suggestion.suggested_document_type || ""}
          onChange={(e) =>
            handlers.onDocumentTypeChange(suggestion.id, e.target.value)
          }
          disabled={disabled || excluded.has("document_type")}
          aria-label="Suggested document type"
          placeholder="Document type"
          className={inputClasses}
        />
      </FieldRow>

      <FieldRow
        suggestion={suggestion}
        fieldKey="created_date"
        excluded={excluded.has("created_date")}
        onToggleField={onToggleField}
      >
        <input
          type="text"
          value={suggestion.suggested_created_date || ""}
          onChange={(e) =>
            handlers.onCreatedDateChange(suggestion.id, e.target.value)
          }
          disabled={disabled || excluded.has("created_date")}
          aria-label="Suggested created date"
          placeholder="YYYY-MM-DD"
          inputMode="numeric"
          pattern="\d{4}-\d{2}-\d{2}"
          title="Use the format YYYY-MM-DD"
          className={inputClasses}
        />
      </FieldRow>

      {suggestion.suggested_custom_fields &&
        suggestion.suggested_custom_fields.length > 0 && (
          <div className="py-2.5">
            <span className="text-xs font-medium text-muted">
              Custom fields
            </span>
            <div className="mt-2 space-y-1.5">
              {suggestion.suggested_custom_fields.map((field) => (
                <label
                  key={field.id}
                  className="flex cursor-pointer items-start gap-2 text-sm"
                >
                  <input
                    type="checkbox"
                    checked={field.isSelected}
                    onChange={() =>
                      handlers.onCustomFieldSuggestionToggle(
                        suggestion.id,
                        field.id
                      )
                    }
                    disabled={disabled}
                    className="mt-0.5 h-4 w-4 cursor-pointer rounded accent-primary"
                  />
                  <span className="min-w-0 break-words">
                    <span className="font-medium">{field.name}:</span>{" "}
                    {String(field.value)}
                  </span>
                </label>
              ))}
            </div>
          </div>
        )}
    </div>
  );
};

export default SuggestionFields;

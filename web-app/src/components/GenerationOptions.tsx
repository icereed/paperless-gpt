import { CheckIcon, SparklesIcon } from "@heroicons/react/24/outline";
import classNames from "classnames";
import React from "react";
import Button from "./ui/Button";

export interface GenerationFlags {
  titles: boolean;
  tags: boolean;
  correspondents: boolean;
  documentTypes: boolean;
  createdDate: boolean;
  customFields: boolean;
}

const fieldChips: { key: keyof GenerationFlags; label: string }[] = [
  { key: "titles", label: "Title" },
  { key: "tags", label: "Tags" },
  { key: "correspondents", label: "Correspondent" },
  { key: "documentTypes", label: "Document type" },
  { key: "createdDate", label: "Created date" },
  { key: "customFields", label: "Custom fields" },
];

interface GenerationOptionsProps {
  flags: GenerationFlags;
  onChange: (flags: GenerationFlags) => void;
  selectedCount: number;
  onGenerate: () => void;
}

/**
 * One decision cluster: which fields the AI should suggest, then one primary
 * action that carries the current selection count.
 */
const GenerationOptions: React.FC<GenerationOptionsProps> = ({
  flags,
  onChange,
  selectedCount,
  onGenerate,
}) => {
  const anyField = fieldChips.some(({ key }) => flags[key]);

  return (
    <div className="mt-6 flex flex-wrap items-end justify-between gap-4 rounded-lg border border-line bg-surface p-4">
      <fieldset className="min-w-0">
        <legend className="text-xs font-medium text-muted">
          Fields to suggest
        </legend>
        <div className="mt-2 flex flex-wrap gap-2">
          {fieldChips.map(({ key, label }) => {
            const checked = flags[key];
            return (
              <label
                key={key}
                className={classNames(
                  "inline-flex cursor-pointer select-none items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm",
                  "transition-colors duration-150 ease-out-quart",
                  "focus-within:outline focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-primary",
                  checked
                    ? "border-primary bg-primary-tint text-ink"
                    : "border-line text-muted hover:border-faint hover:text-ink"
                )}
              >
                <input
                  type="checkbox"
                  className="sr-only"
                  checked={checked}
                  onChange={(e) => onChange({ ...flags, [key]: e.target.checked })}
                />
                {checked && (
                  <CheckIcon className="h-3.5 w-3.5" aria-hidden="true" />
                )}
                {label}
              </label>
            );
          })}
        </div>
      </fieldset>

      <Button
        variant="primary"
        onClick={onGenerate}
        disabled={selectedCount === 0 || !anyField}
        title={
          selectedCount === 0
            ? "Select at least one document"
            : !anyField
              ? "Select at least one field to suggest"
              : undefined
        }
      >
        <SparklesIcon className="h-4 w-4" aria-hidden="true" />
        Generate suggestions
        {selectedCount > 0 && <span aria-hidden="true">·</span>}
        {selectedCount > 0 && (
          <span>
            {selectedCount} {selectedCount === 1 ? "document" : "documents"}
          </span>
        )}
      </Button>
    </div>
  );
};

export default GenerationOptions;

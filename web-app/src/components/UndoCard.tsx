// UndoCard.tsx
import React from 'react';
import { Tooltip } from 'react-tooltip';

interface ModificationProps {
  ID: number;
  DocumentID: number;
  DateChanged: string;
  ModField: string;
  PreviousValue: string;
  NewValue: string;
  Undone: boolean;
  UndoneDate: string | null;
  onUndo: (id: number) => void;
  onReprocess?: (documentId: number) => void;
  paperlessUrl: string;
}

const formatDate = (dateString: string | null): string => {
  if (!dateString) return '';

  try {
    const date = new Date(dateString);
    // Check if date is valid
    if (isNaN(date.getTime())) {
      return 'Invalid date';
    }
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
  } catch {
    return 'Invalid date';
  }
};

const buildPaperlessUrl = (paperlessUrl: string, documentId: number): string => {
  return `${paperlessUrl}/documents/${documentId}/details`;
};

const UndoCard: React.FC<ModificationProps> = ({
  ID,
  DocumentID,
  DateChanged,
  ModField,
  PreviousValue,
  NewValue,
  Undone,
  UndoneDate,
  onUndo,
  onReprocess,
  paperlessUrl,
}) => {
  const formatValue = (value: string, field: string) => {
    if (field === 'tags') {
      try {
        const tags = JSON.parse(value) as string[];
        return (
          <div className="flex flex-wrap gap-1">
            {tags.map((tag) => (
              <span
                key={tag}
                className="bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 text-xs font-medium px-2.5 py-0.5 rounded-full"
              >
                {tag}
              </span>
            ))}
          </div>
        );
      } catch {
        return value;
      }
    } else if (field.toLowerCase().includes('date')) {
      return formatDate(value);
    }
    return value;
  };

  const previousTooltipProps =
    ModField !== "tags" && PreviousValue.length > 100
      ? { "data-tooltip-id": `tooltip-${ID}-prev` }
      : {};
  const newTooltipProps =
    ModField !== "tags" && NewValue.length > 100
      ? { "data-tooltip-id": `tooltip-${ID}-new` }
      : {};

  return (
    <div className="relative rounded-3xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <div className="flex flex-col gap-5 xl:flex-row xl:items-start xl:justify-between">
        <div className="min-w-0 flex-1">
          <div className="grid gap-4 sm:grid-cols-3">
            <div>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                Date Modified
              </div>
              <div className="text-sm text-gray-700 dark:text-gray-300">
                {DateChanged && formatDate(DateChanged)}
              </div>
            </div>
            <div>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                Document
              </div>
              <a
                href={buildPaperlessUrl(paperlessUrl, DocumentID)}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm font-medium text-blue-600 transition hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                #{DocumentID}
              </a>
            </div>
            <div>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                Modified Field
              </div>
              <div className="text-sm text-gray-700 dark:text-gray-300">
                {ModField}
              </div>
            </div>
          </div>

          <div className="mt-5 space-y-3">
            <div className={`rounded-2xl border border-red-100 bg-red-50/60 p-3 dark:border-red-900/40 dark:bg-red-950/20 ${Undone ? "line-through" : ""}`}>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-red-600 dark:text-red-300">
                Previous value
              </div>
              <div
                className="min-w-0 text-sm text-gray-700 dark:text-gray-200"
                {...previousTooltipProps}
              >
                {formatValue(PreviousValue, ModField)}
              </div>
            </div>
            <div className={`rounded-2xl border border-green-100 bg-green-50/60 p-3 dark:border-green-900/40 dark:bg-green-950/20 ${Undone ? "line-through" : ""}`}>
              <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-green-700 dark:text-green-300">
                New value
              </div>
              <div
                className="min-w-0 text-sm text-gray-700 dark:text-gray-200"
                {...newTooltipProps}
              >
                {formatValue(NewValue, ModField)}
              </div>
            </div>
          </div>
          <Tooltip
            id={`tooltip-${ID}-prev`}
            place="bottom"
            className="flex-wrap"
            style={{
              flexWrap: "wrap",
              wordWrap: "break-word",
              zIndex: 10,
              whiteSpace: "pre-line",
              textAlign: "left",
            }}
          >
            {PreviousValue}
          </Tooltip>
          <Tooltip
            id={`tooltip-${ID}-new`}
            place="bottom"
            className="flex-wrap"
            style={{
              flexWrap: "wrap",
              wordWrap: "break-word",
              zIndex: 10,
              whiteSpace: "pre-line",
              textAlign: "left",
            }}
          >
            {NewValue}
          </Tooltip>
        </div>

        <div className="xl:pl-4">
          {onReprocess && (
            <button
              onClick={() => onReprocess(DocumentID)}
              className="mb-2 min-w-[132px] rounded-2xl bg-amber-100 px-4 py-3 text-sm font-medium text-amber-800 transition-colors duration-200 hover:bg-amber-200 dark:bg-amber-900 dark:text-amber-200 dark:hover:bg-amber-800"
            >
              Reprocess
            </button>
          )}
          <button
            onClick={() => onUndo(ID)}
            disabled={Undone}
            className={`min-w-[132px] rounded-2xl px-4 py-3 text-sm font-medium transition-colors duration-200 ${
              Undone
                ? "cursor-not-allowed bg-gray-200 text-gray-500 dark:bg-gray-700 dark:text-gray-400"
                : "bg-blue-600 text-white hover:bg-blue-700 dark:bg-blue-700 dark:hover:bg-blue-800"
            }`}
          >
            {Undone ? (
              <>
                <span className="block text-xs uppercase tracking-wide">Undone</span>
                <span className="mt-1 block text-xs">{formatDate(UndoneDate)}</span>
              </>
            ) : (
              "Undo"
            )}
          </button>
        </div>
      </div>
    </div>
  );
};

export default UndoCard;
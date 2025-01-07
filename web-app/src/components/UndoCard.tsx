// UndoCard.tsx
import React from 'react';
import { Tooltip } from 'react-tooltip'

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

  return (
    <div className="relative bg-white dark:bg-gray-800 p-4 rounded-md shadow-md">
      <div className="grid grid-cols-6">
        <div className="col-span-5"> {/* Left content */}
          <div className="grid grid-cols-3 gap-4 mb-4">
            <div className="">
              <div className="text-xs uppercase text-gray-500 dark:text-gray-400 font-semibold mb-1">
                Date Modified
              </div>
              <div className="text-sm text-gray-700 dark:text-gray-300">
                {DateChanged && formatDate(DateChanged)}
              </div>
            </div>
            <div className="">
              <a
                href={buildPaperlessUrl(paperlessUrl, DocumentID)}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300"
              >
                <div className="text-xs uppercase text-gray-500 dark:text-gray-400 font-semibold mb-1">
                  Document ID
                </div>
                <div className="text-sm text-gray-700 dark:text-gray-300">
                  {DocumentID}
                </div>
              </a>
            </div>

            <div className="">
              <div className="text-xs uppercase text-gray-500 dark:text-gray-400 font-semibold mb-1">
                Modified Field
              </div>
              <div className="text-sm text-gray-700 dark:text-gray-300">
                {ModField}
              </div>
            </div>
          </div>
          <div className="mt-3">
            <div className="mt-2 space-y-2">
              <div className={`text-sm flex flex-nowrap ${Undone ? 'line-through' : ''}`}>
                <span className="text-red-500 dark:text-red-400">Previous: &nbsp;</span>
                <span
                  className="text-gray-600 dark:text-gray-300 truncate overflow-hidden flex-shrink-0 whitespace-nowrap flex-1 max-w-full group relative"
                  { // Add tooltip if value is too long and not tags
                    ...(ModField !== 'tags' && PreviousValue.length > 100 ? {
                    'data-tooltip-id': `tooltip-${ID}-prev`
                  } : {})}
                >
                  {formatValue(PreviousValue, ModField)}
                </span>
              </div>
              <div className={`text-sm flex flex-nowrap ${Undone ? 'line-through' : ''}`}>
                <span className="text-green-500 dark:text-green-400">New: &nbsp;</span>
                <span
                  className="text-gray-600 dark:text-gray-300 truncate overflow-hidden flex-shrink-0 whitespace-nowrap flex-1 max-w-full group relative"
                  { // Add tooltip if value is too long and not tags
                    ...(ModField !== 'tags' && NewValue.length > 100 ? {
                    'data-tooltip-id': `tooltip-${ID}-new`
                  } : {})}
                >
                  {formatValue(NewValue, ModField)}
                </span>
              </div>
            </div>
            <Tooltip 
              id={`tooltip-${ID}-prev`} 
              place="bottom"
              className="flex-wrap"
              style={{
                flexWrap: 'wrap',
                wordWrap: 'break-word',
                zIndex: 10,
                whiteSpace: 'pre-line',
                textAlign: 'left',
              }}
            >
              {PreviousValue}
            </Tooltip>
            <Tooltip 
              id={`tooltip-${ID}-new`} 
              place="bottom"
              className="flex-wrap"
              style={{
                flexWrap: 'wrap',
                wordWrap: 'break-word',
                zIndex: 10,
                whiteSpace: 'pre-line',
                textAlign: 'left',
              }}
            >
              {NewValue}
            </Tooltip>
          </div>
        </div>
        <div className="grid place-items-center"> {/* Button content */}
          <button
            onClick={() => onUndo(ID)}
            disabled={Undone}
            className={`mt-2 mb-2 p-4 text-sm font-medium rounded-md min-w-[100px] max-w-[150px] text-center break-words ${Undone
              ? 'bg-gray-300 dark:bg-gray-700 text-gray-500 dark:text-gray-400 cursor-not-allowed'
              : 'bg-blue-500 dark:bg-blue-600 text-white hover:bg-blue-600 dark:hover:bg-blue-700'
              } transition-colors duration-200`}
          >
            {Undone ? (
              <>
                <span className="block text-xs">Undone on</span>
                <span className="block text-xs">{formatDate(UndoneDate)}</span>
              </>
            ) : (
              'Undo'
            )}
          </button>
        </div>
      </div>
    </div>
  );
};

export default UndoCard;
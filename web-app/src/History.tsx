import React, { useEffect, useState } from 'react';
import UndoCard from './components/UndoCard';

interface ModificationHistory {
  ID: number;
  DocumentID: number;
  BatchID?: number | null;
  DateChanged: string;
  ModField: string;
  PreviousValue: string;
  NewValue: string;
  Undone: boolean;
  UndoneDate: string | null;
}

interface IntegrationActionLog {
  ID: number;
  DocumentID: number;
  Provider: string;
  ActionType: string;
  Status: string;
  ExternalID?: string;
  ExternalURL?: string;
  ErrorMessage?: string;
}

interface ApplyBatchHistoryItem {
  id: number;
  started_at: string;
  ended_at?: string;
  doc_count: number;
  summary: string;
  undone: boolean;
  undone_at?: string;
  modifications: ModificationHistory[];
  integration_actions: IntegrationActionLog[];
}

interface PaginatedResponse {
  items: ApplyBatchHistoryItem[];
  totalItems: number;
  totalPages: number;
  currentPage: number;
  pageSize: number;
}

const formatDate = (dateString?: string | null): string => {
  if (!dateString) return '';
  const date = new Date(dateString);
  if (Number.isNaN(date.getTime())) return 'Invalid date';
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
};

const History: React.FC = () => {
  const [batches, setBatches] = useState<ApplyBatchHistoryItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [paperlessUrl, setPaperlessUrl] = useState<string>('');
  const [currentPage, setCurrentPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalItems, setTotalItems] = useState(0);
  const pageSize = 20;

  // Get Paperless URL
  useEffect(() => {
    const fetchUrl = async () => {
      try {
        const response = await fetch('./api/paperless-url');
        if (!response.ok) {
          throw new Error('Failed to fetch public URL');
        }
        const { url } = await response.json();
        setPaperlessUrl(url);
      } catch (err) {
        console.error('Error fetching Paperless URL:', err);
      }
    };
    
    fetchUrl();
  }, []);

  // Get modifications with pagination
  useEffect(() => {
    fetchModifications(currentPage);
  }, [currentPage]);

  const fetchModifications = async (page: number) => {
    setLoading(true);
    try {
      const response = await fetch(`./api/apply-batches?page=${page}&pageSize=${pageSize}`);
      if (!response.ok) {
        throw new Error('Failed to fetch modifications');
      }
      const data: PaginatedResponse = await response.json();
      setBatches(data.items);
      setTotalPages(data.totalPages);
      setTotalItems(data.totalItems);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error occurred');
    } finally {
      setLoading(false);
    }
  };

  const handleUndo = async (id: number) => {
    try {
      const response = await fetch(`./api/undo-modification/${id}`, {
        method: 'POST',
      });
      
      if (!response.ok) {
        throw new Error('Failed to undo modification');
      }
  
      // Use ISO 8601 format for consistency
      const now = new Date().toISOString();
      
      setBatches(items => items.map(batch => ({
        ...batch,
        modifications: batch.modifications.map(mod =>
          mod.ID === id
            ? { ...mod, Undone: true, UndoneDate: now }
            : mod
        ),
      })));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to undo modification');
    }
  };

  const handleUndoBatch = async (id: number) => {
    try {
      const response = await fetch(`./api/undo-batch/${id}`, { method: 'POST' });
      if (!response.ok) {
        throw new Error('Failed to undo batch');
      }
      const now = new Date().toISOString();
      setBatches(items => items.map(batch =>
        batch.id === id
          ? {
              ...batch,
              undone: true,
              undone_at: now,
              modifications: batch.modifications.map(mod => ({
                ...mod,
                Undone: true,
                UndoneDate: mod.UndoneDate || now,
              })),
            }
          : batch
      ));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to undo batch');
    }
  };

  const handleReprocess = async (documentId: number) => {
    try {
      const response = await fetch(`./api/documents/${documentId}/reprocess`, { method: 'POST' });
      if (!response.ok) {
        throw new Error('Failed to reprocess document');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to reprocess document');
    }
  };

  if (loading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-500" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 text-center text-red-500 dark:text-red-400">
        Error: {error}
      </div>
    );
  }

  return (
    <div className="modification-history mx-auto max-w-6xl px-6 py-8">
      <div className="mb-6 rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div className="max-w-3xl">
            <p className="text-sm font-semibold uppercase tracking-[0.2em] text-blue-600 dark:text-blue-400">
              History
            </p>
            <h1 className="mt-2 text-3xl font-bold text-gray-900 dark:text-gray-100">
              Modification history
            </h1>
            <p className="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
              Review document changes made by Paperless GPT and undo any Paperless
              metadata update that should be rolled back. External integration side
              effects are shown for reference and are not deleted automatically.
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800">
              <p className="text-sm text-gray-500 dark:text-gray-400">Tracked changes</p>
              <p className="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">
                {totalItems}
              </p>
            </div>
            <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800">
              <p className="text-sm text-gray-500 dark:text-gray-400">Undo note</p>
              <p className="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">
                Undo rewrites Paperless metadata only
              </p>
            </div>
          </div>
        </div>
      </div>
      {batches.length === 0 ? (
        <div className="rounded-3xl border border-dashed border-gray-300 bg-white px-6 py-12 text-center text-gray-500 shadow-sm dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400">
          No modifications found.
        </div>
      ) : (
        <>
          <div className="mb-6 grid gap-4">
            {batches.map((batch) => (
              <div key={batch.id} className="rounded-3xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
                <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div>
                    <p className="text-xs font-semibold uppercase tracking-wide text-blue-600 dark:text-blue-400">
                      Apply batch #{batch.id}
                    </p>
                    <h2 className="mt-1 text-lg font-semibold text-gray-900 dark:text-gray-100">
                      {batch.summary || `${batch.doc_count} document(s) applied`}
                    </h2>
                    <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                      Started {formatDate(batch.started_at)}
                      {batch.ended_at ? ` · Ended ${formatDate(batch.ended_at)}` : ''}
                    </p>
                  </div>
                  <button
                    onClick={() => handleUndoBatch(batch.id)}
                    disabled={batch.undone}
                    className="rounded-2xl bg-blue-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-gray-300 disabled:text-gray-500 dark:disabled:bg-gray-700"
                  >
                    {batch.undone ? `Batch undone ${batch.undone_at ? formatDate(batch.undone_at) : ''}` : 'Undo entire batch'}
                  </button>
                </div>

                {batch.integration_actions.length > 0 && (
                  <div className="mb-4 rounded-2xl border border-gray-200 bg-gray-50 p-3 dark:border-gray-700 dark:bg-gray-800">
                    <p className="text-sm font-semibold text-gray-700 dark:text-gray-200">Integration side effects</p>
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      Undo rewrites Paperless metadata only. Firefly transactions/attachments,
                      Jobber expenses, and Google Drive files are not deleted automatically.
                    </p>
                    <div className="mt-2 space-y-1 text-sm text-gray-600 dark:text-gray-300">
                      {batch.integration_actions.map((action) => (
                        <div key={action.ID} className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{action.Provider}</span>
                          <span>{action.ActionType}</span>
                          <span className={action.Status === 'success' ? 'text-green-600' : 'text-red-600'}>
                            {action.Status}
                          </span>
                          {action.ExternalURL && (
                            <a className="underline" href={action.ExternalURL} target="_blank" rel="noopener noreferrer">
                              Open
                            </a>
                          )}
                          {action.ErrorMessage && <span>{action.ErrorMessage}</span>}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div className="grid gap-4">
                  {batch.modifications.map((modification) => (
                    <UndoCard
                      key={modification.ID}
                      {...modification}
                      onUndo={handleUndo}
                      onReprocess={handleReprocess}
                      paperlessUrl={paperlessUrl}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
          <div className="flex flex-col gap-3 rounded-3xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center text-sm text-gray-500 dark:text-gray-400">
              {totalItems > 0 && (
                <span>
                  Showing {((currentPage - 1) * pageSize) + 1} to {Math.min(currentPage * pageSize, totalItems)} of {totalItems} results
                </span>
              )}
            </div>
            <div className="flex items-center space-x-2">
              <button
                onClick={() => setCurrentPage(page => Math.max(1, page - 1))}
                disabled={currentPage === 1}
                className={`px-3 py-1 rounded-md ${
                  currentPage === 1
                    ? 'bg-gray-100 text-gray-400 cursor-not-allowed dark:bg-gray-800'
                    : 'bg-blue-500 text-white hover:bg-blue-600 dark:bg-blue-600 dark:hover:bg-blue-700'
                }`}
              >
                Previous
              </button>
              <span className="text-sm text-gray-600 dark:text-gray-300">
                Page {currentPage} of {totalPages}
              </span>
              <button
                onClick={() => setCurrentPage(page => Math.min(totalPages, page + 1))}
                disabled={currentPage === totalPages}
                className={`px-3 py-1 rounded-md ${
                  currentPage === totalPages
                    ? 'bg-gray-100 text-gray-400 cursor-not-allowed dark:bg-gray-800'
                    : 'bg-blue-500 text-white hover:bg-blue-600 dark:bg-blue-600 dark:hover:bg-blue-700'
                }`}
              >
                Next
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
};

export default History;

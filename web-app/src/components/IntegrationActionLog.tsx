import React, { useCallback, useEffect, useState } from 'react';
import ArrowPathIcon from '@heroicons/react/24/outline/ArrowPathIcon';

interface ActionLogEntry {
  ID: number;
  DocumentID: number;
  Provider: string;
  ActionType: string;
  Status: string;
  ExternalID?: string;
  ExternalURL?: string;
  RequestSummary?: string;
  ResponseSummary?: string;
  ErrorMessage?: string;
  CreatedAt: string;
}

interface ActionLogPage {
  items: ActionLogEntry[];
  totalItems: number;
  totalPages: number;
  currentPage: number;
  pageSize: number;
}

const PAGE_SIZE = 20;

const IntegrationActionLog: React.FC = () => {
  const [page, setPage] = useState(1);
  const [data, setData] = useState<ActionLogPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [providerFilter, setProviderFilter] = useState('');

  const fetchPage = useCallback(async (p: number, provider: string) => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ page: String(p), pageSize: String(PAGE_SIZE) });
      if (provider) params.set('provider', provider);
      const res = await fetch(`./api/integrations/action-log?${params.toString()}`);
      if (!res.ok) {
        throw new Error(`Request failed: ${res.status}`);
      }
      const json: ActionLogPage = await res.json();
      setData(json);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchPage(page, providerFilter);
  }, [fetchPage, page, providerFilter]);

  const handleProviderChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    setProviderFilter(e.target.value);
    setPage(1);
  };

  const statusBadge = (status: string) => {
    const base = 'inline-block rounded-full px-2 py-0.5 text-xs font-medium';
    if (status === 'success') return <span className={`${base} bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300`}>success</span>;
    return <span className={`${base} bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300`}>{status}</span>;
  };

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-5">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4">
        <h2 className="text-xl font-semibold text-gray-700 dark:text-gray-300">Integration action log</h2>
        <div className="flex items-center gap-2">
          <select
            value={providerFilter}
            onChange={handleProviderChange}
            className="rounded border border-gray-300 px-2 py-1.5 text-sm dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
          >
            <option value="">All providers</option>
            <option value="jobber">Jobber</option>
            <option value="firefly">Firefly III</option>
            <option value="google_drive">Google Drive</option>
          </select>
          <button
            onClick={() => fetchPage(page, providerFilter)}
            disabled={loading}
            title="Refresh"
            className="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
          >
            <ArrowPathIcon className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-3 rounded border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-700 dark:bg-red-900/30 dark:text-red-300">
          {error}
        </div>
      )}

      {!data || data.items.length === 0 ? (
        <p className="text-sm text-gray-500 dark:text-gray-400">
          {loading ? 'Loading…' : 'No integration actions recorded yet.'}
        </p>
      ) : (
        <>
          <div className="overflow-x-auto">
            <table className="min-w-full text-sm">
              <thead>
                <tr className="border-b border-gray-200 dark:border-gray-700 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                  <th className="pb-2 pr-4">Time</th>
                  <th className="pb-2 pr-4">Provider</th>
                  <th className="pb-2 pr-4">Action</th>
                  <th className="pb-2 pr-4">Doc ID</th>
                  <th className="pb-2 pr-4">Status</th>
                  <th className="pb-2">Details</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 dark:divide-gray-700">
                {data.items.map((entry) => (
                  <tr key={entry.ID} className="hover:bg-gray-50 dark:hover:bg-gray-700/40">
                    <td className="py-2 pr-4 whitespace-nowrap text-gray-500 dark:text-gray-400">
                      {new Date(entry.CreatedAt).toLocaleString()}
                    </td>
                    <td className="py-2 pr-4">{formatProvider(entry.Provider)}</td>
                    <td className="py-2 pr-4">{formatAction(entry.ActionType)}</td>
                    <td className="py-2 pr-4">{entry.DocumentID}</td>
                    <td className="py-2 pr-4">{statusBadge(entry.Status)}</td>
                    <td className="py-2 max-w-xs">
                      {entry.ErrorMessage ? (
                        <span className="text-red-700 dark:text-red-400 break-words">{entry.ErrorMessage}</span>
                      ) : entry.ExternalID ? (
                        entry.ExternalURL ? (
                          <a
                            href={entry.ExternalURL}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-blue-600 hover:underline dark:text-blue-400"
                          >
                            {entry.ExternalID}
                          </a>
                        ) : (
                          <span className="text-gray-600 dark:text-gray-300">{entry.ExternalID}</span>
                        )
                      ) : (
                        <span className="text-gray-400 dark:text-gray-500">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {data.totalPages > 1 && (
            <div className="mt-4 flex items-center justify-between text-sm text-gray-500 dark:text-gray-400">
              <span>
                Page {data.currentPage} of {data.totalPages} ({data.totalItems} entries)
              </span>
              <div className="flex gap-1">
                <button
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={page <= 1 || loading}
                  className="rounded px-3 py-1 border border-gray-300 hover:bg-gray-100 disabled:opacity-40 dark:border-gray-600 dark:hover:bg-gray-700"
                >
                  Previous
                </button>
                <button
                  onClick={() => setPage((p) => Math.min(data.totalPages, p + 1))}
                  disabled={page >= data.totalPages || loading}
                  className="rounded px-3 py-1 border border-gray-300 hover:bg-gray-100 disabled:opacity-40 dark:border-gray-600 dark:hover:bg-gray-700"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default IntegrationActionLog;

function formatProvider(provider: string): string {
  switch (provider) {
    case 'jobber':
      return 'Jobber';
    case 'firefly':
      return 'Firefly III';
    case 'google_drive':
      return 'Google Drive';
    default:
      return provider.replace('_', ' ');
  }
}

function formatAction(action: string): string {
  return action.replace(/_/g, ' ');
}

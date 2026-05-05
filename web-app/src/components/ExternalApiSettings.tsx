import React, { useCallback, useEffect, useMemo, useState } from 'react';

interface ExternalApiKeyStatus {
  configured: boolean;
  source?: string;
  base_url: string;
  local_base_url?: string;
  openapi_url: string;
  local_openapi_url?: string;
  header_name: string;
  api_key?: string;
}

const emptyStatus: ExternalApiKeyStatus = {
  configured: false,
  base_url: '',
  local_base_url: '',
  openapi_url: '',
  local_openapi_url: '',
  header_name: 'X-API-Key',
};

const ExternalApiSettings: React.FC = () => {
  const [status, setStatus] = useState<ExternalApiKeyStatus>(emptyStatus);
  const [generatedKey, setGeneratedKey] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [isGenerating, setIsGenerating] = useState(false);
  const [isRevoking, setIsRevoking] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await fetch('./api/external-api-key');
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to load external API status');
      }
      setStatus(payload);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load external API status');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  const hostPortHint = useMemo(() => {
    const urlForHint = status.local_base_url || status.base_url || window.location.origin;
    try {
      const url = new URL(urlForHint);
      return `${url.hostname}:${url.port || (url.protocol === 'https:' ? '443' : '80')}`;
    } catch {
      return '<local-ip>:8080';
    }
  }, [status.base_url, status.local_base_url]);

  const generateKey = async () => {
    setIsGenerating(true);
    setError(null);
    setMessage(null);
    try {
      const response = await fetch('./api/external-api-key/generate', { method: 'POST' });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to generate API key');
      }
      setStatus(payload);
      setGeneratedKey(payload.api_key || '');
      setMessage('API key generated. Copy it now; it will not be shown again.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to generate API key');
    } finally {
      setIsGenerating(false);
    }
  };

  const revokeKey = async () => {
    if (!window.confirm('Revoke the external API key? bricoprohq will stop connecting until you generate and save a new key.')) {
      return;
    }
    setIsRevoking(true);
    setError(null);
    setMessage(null);
    try {
      const response = await fetch('./api/external-api-key', { method: 'DELETE' });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to revoke API key');
      }
      setStatus(payload);
      setGeneratedKey('');
      setMessage('External API key revoked.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke API key');
    } finally {
      setIsRevoking(false);
    }
  };

  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setMessage(`${label} copied.`);
    } catch {
      setError(`Could not copy ${label.toLowerCase()}. Select and copy it manually.`);
    }
  };

  return (
    <section className="rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.2em] text-gray-500 dark:text-gray-400">
            Advanced
          </p>
          <h2 className="mt-2 text-2xl font-bold text-gray-900 dark:text-gray-100">
            Read-only external API (<code>/api/external/v1</code>)
          </h2>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-600 dark:text-gray-300">
            A separate, read-only namespace for custom dashboards or scripts that want a structured view
            of pending documents, OCR jobs, and integration status without needing the full <code>/api/*</code>
            surface. <strong>Bricopro HQ does not use this</strong> – use the “Connect Bricopro HQ” card above instead.
          </p>
        </div>
        <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${status.configured ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-200' : 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'}`}>
          {status.configured ? `Enabled${status.source ? ` (${status.source})` : ''}` : 'Disabled'}
        </span>
      </div>

      {error && <div className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/30 dark:text-red-200">{error}</div>}
      {message && <div className="mt-4 rounded-lg bg-green-50 p-3 text-sm text-green-700 dark:bg-green-900/30 dark:text-green-200">{message}</div>}

      <div className="mt-6 grid gap-4 md:grid-cols-3">
        <InfoBox label="Local IP / port" value={hostPortHint} />
        <InfoBox label="Local API URL" value={status.local_base_url || status.base_url || 'Loading...'} onCopy={(status.local_base_url || status.base_url) ? () => copy(status.local_base_url || status.base_url, 'Local API URL') : undefined} />
        <InfoBox label="OpenAPI URL" value={status.local_openapi_url || status.openapi_url || 'Loading...'} onCopy={(status.local_openapi_url || status.openapi_url) ? () => copy(status.local_openapi_url || status.openapi_url, 'OpenAPI URL') : undefined} />
      </div>

      {generatedKey && (
        <div className="mt-6 rounded-2xl border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/30">
          <p className="text-sm font-semibold text-amber-900 dark:text-amber-100">New API key - copy now</p>
          <div className="mt-2 flex flex-col gap-2 md:flex-row">
            <input readOnly value={generatedKey} className="flex-1 rounded border border-amber-300 px-3 py-2 font-mono text-sm dark:border-amber-700 dark:bg-gray-950 dark:text-gray-100" />
            <button type="button" onClick={() => copy(generatedKey, 'API key')} className="rounded bg-amber-600 px-4 py-2 text-sm font-semibold text-white hover:bg-amber-700">
              Copy key
            </button>
          </div>
          <p className="mt-2 text-xs text-amber-800 dark:text-amber-200">
            For security, Paperless GPT stores only an encrypted copy and will not display this key again.
          </p>
        </div>
      )}

      <div className="mt-6 rounded-2xl bg-gray-50 p-4 dark:bg-gray-950">
        <h3 className="font-semibold text-gray-900 dark:text-gray-100">Calling the read-only API</h3>
        <ol className="mt-2 list-decimal space-y-1 pl-5 text-sm text-gray-700 dark:text-gray-300">
          <li>Send the API key in the <code>X-API-Key</code> header (or <code>Authorization: Bearer …</code>).</li>
          <li>Base path: <code>{status.local_base_url || status.base_url || '/api/external/v1'}</code></li>
          <li>OpenAPI spec: <code>{status.local_openapi_url || status.openapi_url || '/api/external/v1/openapi.json'}</code></li>
          <li>Local listener hint: <code>{hostPortHint}</code></li>
        </ol>
      </div>

      <div className="mt-6 flex flex-wrap gap-3">
        <button
          type="button"
          disabled={isLoading || isGenerating || status.source === 'environment'}
          onClick={generateKey}
          className="rounded bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {isGenerating ? 'Generating...' : status.configured ? 'Rotate API key' : 'Generate API key'}
        </button>
        <button
          type="button"
          disabled={isLoading || isRevoking || !status.configured || status.source === 'environment'}
          onClick={revokeKey}
          className="rounded border border-red-300 px-4 py-2 text-sm font-semibold text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-red-800 dark:text-red-200 dark:hover:bg-red-950"
        >
          {isRevoking ? 'Revoking...' : 'Revoke key'}
        </button>
      </div>
      {status.source === 'environment' && (
        <p className="mt-3 text-xs text-gray-500 dark:text-gray-400">
          This key is configured with <code>PAPERLESS_GPT_API_KEY</code> or <code>EXTERNAL_API_KEY</code>. Rotate or revoke it in your container/server environment.
        </p>
      )}
    </section>
  );
};

const InfoBox: React.FC<{ label: string; value: string; onCopy?: () => void }> = ({ label, value, onCopy }) => (
  <div className="rounded-2xl border border-gray-200 p-4 dark:border-gray-800">
    <p className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{label}</p>
    <div className="mt-2 flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate text-sm text-gray-900 dark:text-gray-100">{value}</code>
      {onCopy && (
        <button type="button" onClick={onCopy} className="text-xs font-semibold text-indigo-600 hover:text-indigo-700 dark:text-indigo-300">
          Copy
        </button>
      )}
    </div>
  </div>
);

export default ExternalApiSettings;

import React, { useCallback, useEffect, useMemo, useState } from 'react';

interface BricoproHQConnectorStatus {
  configured: boolean;
  base_url: string;
  local_base_url?: string;
  health_url: string;
  documents_url: string;
  header_name: string;
  api_key?: string;
  queue_tag: string;
  api_version: string;
  last_used_at?: string;
}

const emptyStatus: BricoproHQConnectorStatus = {
  configured: false,
  base_url: '',
  local_base_url: '',
  health_url: '',
  documents_url: '',
  header_name: 'X-API-Key',
  queue_tag: '',
  api_version: 'v1',
};

const ConnectorIntegrations: React.FC = () => {
  const [status, setStatus] = useState<BricoproHQConnectorStatus>(emptyStatus);
  const [generatedKey, setGeneratedKey] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [isGenerating, setIsGenerating] = useState(false);
  const [isRevoking, setIsRevoking] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await fetch('./api/bricoprohq-connector');
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to load BricoproHQ connector status');
      }
      setStatus(payload);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load BricoproHQ connector status');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  const baseURL = status.local_base_url || status.base_url;

  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setMessage(`${label} copied.`);
      window.setTimeout(() => setMessage(null), 2500);
    } catch {
      setError(`Could not copy ${label.toLowerCase()}. Select and copy it manually.`);
    }
  };

  const hostPortHint = useMemo(() => {
    const urlForHint = baseURL || window.location.origin;
    try {
      const url = new URL(urlForHint);
      return `${url.hostname}:${url.port || (url.protocol === 'https:' ? '443' : '80')}`;
    } catch {
      return '<local-ip>:8080';
    }
  }, [baseURL]);

  const generateKey = async () => {
    setIsGenerating(true);
    setError(null);
    setMessage(null);
    try {
      const response = await fetch('./api/bricoprohq-connector/key', { method: 'POST' });
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
    if (!window.confirm('Revoke the BricoproHQ connector API key? BricoproHQ will stop connecting until you generate and save a new key.')) {
      return;
    }
    setIsRevoking(true);
    setError(null);
    setMessage(null);
    try {
      const response = await fetch('./api/bricoprohq-connector/key', { method: 'DELETE' });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to revoke API key');
      }
      setStatus(payload);
      setGeneratedKey('');
      setMessage('BricoproHQ connector API key revoked.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke API key');
    } finally {
      setIsRevoking(false);
    }
  };

  return (
    <section className="rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">
            External integrations
          </p>
          <h2 className="mt-2 text-2xl font-bold text-gray-900 dark:text-gray-100">
            Connect Bricopro HQ to Paperless GPT
          </h2>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-gray-600 dark:text-gray-300">
            Generate one local connector API key, then paste that key and the Paperless GPT URL into
            <em> Bricopro HQ → Settings → Integrations → Paperless-GPT</em>. No OAuth, admin login,
            bearer token, or auth mode is required.
          </p>
        </div>
        <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${status.configured ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-200' : 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'}`}>
          {status.configured ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      {error && (
        <div className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/30 dark:text-red-200">
          {error}
        </div>
      )}
      {message && (
        <div className="mt-4 rounded-lg bg-green-50 p-3 text-sm text-green-700 dark:bg-green-900/30 dark:text-green-200">
          {message}
        </div>
      )}

      <div className="mt-6 grid gap-4 md:grid-cols-3">
        <Field
          label="Paperless-GPT URL"
          hint="Paste this URL into BricoproHQ."
          value={baseURL || (isLoading ? 'Loading...' : '')}
          onCopy={baseURL ? () => copy(baseURL, 'Paperless-GPT URL') : undefined}
        />
        <Field
          label="Local IP / port"
          hint="Use this if BricoproHQ is running on the same LAN."
          value={hostPortHint}
        />
        <Field
          label="Queue tag"
          hint="Documents with this Paperless tag are returned to BricoproHQ."
          value={status.queue_tag || 'paperless-gpt'}
        />
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

      <div className="mt-6 rounded-2xl border border-gray-200 p-4 dark:border-gray-800">
        <h3 className="font-semibold text-gray-900 dark:text-gray-100">Bricopro HQ settings</h3>
        <table className="mt-3 w-full text-sm">
          <tbody className="divide-y divide-gray-200 dark:divide-gray-800">
            <Row label="Paperless-GPT URL" value={baseURL} onCopy={baseURL ? () => copy(baseURL, 'Paperless-GPT URL') : undefined} />
            <Row label="API Key" value={generatedKey || (status.configured ? '(already generated - rotate to show a new key)' : '(generate a key first)')} onCopy={generatedKey ? () => copy(generatedKey, 'API key') : undefined} />
            <Row
              label="Tag for queue"
              value={status.queue_tag || 'paperless-gpt'}
              hint="Override with the MANUAL_TAG env var if needed."
            />
          </tbody>
        </table>
      </div>

      <div className="mt-6 flex flex-wrap gap-3">
        <button
          type="button"
          disabled={isLoading || isGenerating}
          onClick={generateKey}
          className="rounded bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {isGenerating ? 'Generating...' : status.configured ? 'Rotate API key' : 'Generate API key'}
        </button>
        <button
          type="button"
          disabled={isLoading || isRevoking || !status.configured}
          onClick={revokeKey}
          className="rounded border border-red-300 px-4 py-2 text-sm font-semibold text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-red-800 dark:text-red-200 dark:hover:bg-red-950"
        >
          {isRevoking ? 'Revoking...' : 'Revoke key'}
        </button>
      </div>

      <details className="mt-6 rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800">
        <summary className="cursor-pointer font-semibold text-gray-900 dark:text-gray-100">
          Verify from a terminal
        </summary>
        <p className="mt-2 text-gray-600 dark:text-gray-300">Run this from any host that can reach Paperless GPT:</p>
        <pre className="mt-2 overflow-x-auto rounded bg-gray-900 p-3 text-xs text-gray-100">
{`curl -i \\
  -H "X-API-Key: <api-key>" \\
  ${status.health_url || `${baseURL || '<paperless-gpt-url>'}/api/bricoprohq/v1/health`}`}
        </pre>
        <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
          200 OK with <code>{'{"ok":true}'}</code> means BricoproHQ can connect.
        </p>
      </details>
    </section>
  );
};

const Field: React.FC<{ label: string; hint?: string; value: string; onCopy?: () => void }> = ({
  label,
  hint,
  value,
  onCopy,
}) => (
  <div className="rounded-2xl border border-gray-200 p-4 dark:border-gray-800">
    <p className="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{label}</p>
    <div className="mt-2 flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate text-sm text-gray-900 dark:text-gray-100">{value || '—'}</code>
      {onCopy && (
        <button
          type="button"
          onClick={onCopy}
          className="text-xs font-semibold text-indigo-600 hover:text-indigo-700 dark:text-indigo-300"
        >
          Copy
        </button>
      )}
    </div>
    {hint && <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">{hint}</p>}
  </div>
);

const Row: React.FC<{ label: string; value: string; onCopy?: () => void; hint?: string }> = ({
  label,
  value,
  onCopy,
  hint,
}) => (
  <tr>
    <td className="w-1/3 py-2 pr-4 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
      {label}
    </td>
    <td className="py-2">
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate text-sm text-gray-900 dark:text-gray-100">{value || '—'}</code>
        {onCopy && (
          <button
            type="button"
            onClick={onCopy}
            className="text-xs font-semibold text-indigo-600 hover:text-indigo-700 dark:text-indigo-300"
          >
            Copy
          </button>
        )}
      </div>
      {hint && <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{hint}</p>}
    </td>
  </tr>
);

export default ConnectorIntegrations;

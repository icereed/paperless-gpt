import React, { useCallback, useEffect, useMemo, useState } from 'react';

interface ConnectorAuthStatus {
  auth_token_configured: boolean;
  session_auth_required: boolean;
  basic_auth_configured: boolean;
  base_url: string;
  local_base_url?: string;
  header_name: string;
  header_value_example: string;
  documents_url: string;
  local_documents_url?: string;
  recommended_auth_mode: 'none' | 'bearer' | 'token' | 'x-api-key';
  env_var_name: string;
}

const emptyStatus: ConnectorAuthStatus = {
  auth_token_configured: false,
  session_auth_required: false,
  basic_auth_configured: false,
  base_url: '',
  local_base_url: '',
  header_name: 'Authorization',
  header_value_example: 'Bearer <AUTH_TOKEN>',
  documents_url: '',
  local_documents_url: '',
  recommended_auth_mode: 'none',
  env_var_name: 'AUTH_TOKEN',
};

const ConnectorIntegrations: React.FC = () => {
  const [status, setStatus] = useState<ConnectorAuthStatus>(emptyStatus);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [copyMessage, setCopyMessage] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await fetch('./api/connector-auth-status');
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to load connector status');
      }
      setStatus(payload);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load connector status');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  const baseURL = status.local_base_url || status.base_url;
  const documentsURL = status.local_documents_url || status.documents_url;

  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopyMessage(`${label} copied.`);
      window.setTimeout(() => setCopyMessage(null), 2500);
    } catch {
      setError(`Could not copy ${label.toLowerCase()}. Select and copy it manually.`);
    }
  };

  const recommendedMode: ConnectorAuthStatus['recommended_auth_mode'] = useMemo(() => {
    if (status.auth_token_configured) return 'bearer';
    if (!status.session_auth_required && !status.basic_auth_configured) return 'none';
    return 'bearer';
  }, [status.auth_token_configured, status.session_auth_required, status.basic_auth_configured]);

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
            Bricopro HQ polls <code className="rounded bg-gray-100 px-1 py-0.5 text-xs dark:bg-gray-800">/api/documents</code> to surface
            documents waiting for review. The settings below tell you exactly what to enter
            in <em>Bricopro HQ → Settings → Integrations → Paperless-GPT</em>.
          </p>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/30 dark:text-red-200">
          {error}
        </div>
      )}
      {copyMessage && (
        <div className="mt-4 rounded-lg bg-green-50 p-3 text-sm text-green-700 dark:bg-green-900/30 dark:text-green-200">
          {copyMessage}
        </div>
      )}

      <div className="mt-6 grid gap-4 md:grid-cols-2">
        <Field
          label="Base URL"
          hint="No trailing /api – Bricopro HQ appends that itself."
          value={baseURL || (isLoading ? 'Loading...' : '')}
          onCopy={baseURL ? () => copy(baseURL, 'Base URL') : undefined}
        />
        <Field
          label="Auth Mode"
          hint={authModeHint(recommendedMode, status)}
          value={recommendedMode}
        />
      </div>

      <div className="mt-6 rounded-2xl bg-gray-50 p-4 dark:bg-gray-950">
        <h3 className="font-semibold text-gray-900 dark:text-gray-100">Current state</h3>
        <ul className="mt-2 space-y-1 text-sm text-gray-700 dark:text-gray-300">
          <li>
            <Badge ok={!status.session_auth_required}>
              {status.session_auth_required ? 'Admin login is required' : 'Open access (no admin user)'}
            </Badge>
            {status.session_auth_required && (
              <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                — at least one admin account exists, so the API will refuse anonymous calls.
              </span>
            )}
          </li>
          <li>
            <Badge ok={status.auth_token_configured}>
              {status.auth_token_configured ? 'AUTH_TOKEN is set' : 'AUTH_TOKEN is NOT set'}
            </Badge>
            {!status.auth_token_configured && status.session_auth_required && (
              <span className="ml-2 text-xs text-gray-500 dark:text-gray-400">
                — Bricopro HQ cannot connect until you set <code>AUTH_TOKEN</code> on the container.
              </span>
            )}
          </li>
          {status.basic_auth_configured && (
            <li>
              <Badge ok>HTTP Basic Auth is configured (AUTH_USERNAME/AUTH_PASSWORD)</Badge>
            </li>
          )}
        </ul>
      </div>

      {status.session_auth_required && !status.auth_token_configured && (
        <div className="mt-6 rounded-2xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100">
          <p className="font-semibold">Action required</p>
          <p className="mt-1">
            You have an admin account, so <code>/api/documents</code> requires authentication. Bricopro HQ does not
            sign in with a username/password; it sends a fixed <em>API key</em> in an HTTP header. To let it through:
          </p>
          <ol className="mt-2 list-decimal space-y-1 pl-5">
            <li>
              On the Paperless GPT container, set the env var{' '}
              <code className="rounded bg-amber-100 px-1 py-0.5 dark:bg-amber-900/40">AUTH_TOKEN=&lt;long-random-secret&gt;</code>{' '}
              and restart it.
            </li>
            <li>In Bricopro HQ, paste that same value as the API key and pick Auth Mode <strong>bearer</strong>.</li>
            <li>Use the Base URL shown above.</li>
          </ol>
          <p className="mt-2 text-xs text-amber-800 dark:text-amber-200">
            The token is stored only in your container env – Paperless GPT never displays it back here.
          </p>
        </div>
      )}

      <div className="mt-6 rounded-2xl border border-gray-200 p-4 dark:border-gray-800">
        <h3 className="font-semibold text-gray-900 dark:text-gray-100">Bricopro HQ settings</h3>
        <table className="mt-3 w-full text-sm">
          <tbody className="divide-y divide-gray-200 dark:divide-gray-800">
            <Row label="Base URL" value={baseURL} onCopy={baseURL ? () => copy(baseURL, 'Base URL') : undefined} />
            <Row label="Auth Mode" value={recommendedMode} />
            {recommendedMode === 'bearer' && (
              <Row
                label="API Key"
                value={status.auth_token_configured ? '(value of your AUTH_TOKEN env var)' : '(set AUTH_TOKEN first)'}
              />
            )}
            <Row
              label="Tag for queue"
              value="paperless-gpt"
              hint="Override with the MANUAL_TAG env var if needed."
            />
          </tbody>
        </table>
      </div>

      <details className="mt-6 rounded-2xl border border-gray-200 p-4 text-sm dark:border-gray-800">
        <summary className="cursor-pointer font-semibold text-gray-900 dark:text-gray-100">
          Verify from a terminal
        </summary>
        <p className="mt-2 text-gray-600 dark:text-gray-300">Run this from any host that can reach Paperless GPT:</p>
        <pre className="mt-2 overflow-x-auto rounded bg-gray-900 p-3 text-xs text-gray-100">
{`curl -i \\
  -H "Authorization: Bearer $AUTH_TOKEN" \\
  ${documentsURL || `${baseURL || '<base-url>'}/api/documents`}`}
        </pre>
        <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
          200 OK with a JSON array means Bricopro HQ will be able to connect.
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

const Badge: React.FC<{ ok: boolean; children: React.ReactNode }> = ({ ok, children }) => (
  <span
    className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold ${
      ok
        ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-200'
        : 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-100'
    }`}
  >
    <span aria-hidden>{ok ? '✓' : '!'}</span>
    {children}
  </span>
);

const authModeHint = (
  mode: ConnectorAuthStatus['recommended_auth_mode'],
  status: ConnectorAuthStatus,
): string => {
  if (mode === 'none') {
    return 'No admin user is set up, so the API is open. Use Auth Mode "none" in Bricopro HQ.';
  }
  if (status.auth_token_configured) {
    return 'AUTH_TOKEN is set. Use Auth Mode "bearer" and paste the same value as the API key.';
  }
  return 'Set AUTH_TOKEN on the container to enable the bearer-token bypass for machine integrations.';
};

export default ConnectorIntegrations;

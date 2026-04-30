import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import IntegrationActionLog from './IntegrationActionLog';

interface CustomField {
  id: number;
  name: string;
  data_type: string;
}

interface FieldOption {
  value: string;
  label: string;
}

interface SettingsData {
  jobber_enabled: boolean;
  jobber_client_id: string;
  jobber_client_secret: string;
  jobber_client_secret_configured?: boolean;
  jobber_job_id_field_id: number;
  jobber_job_number_field_id: number;
  jobber_client_field_id: number;
  jobber_job_name_field_id: number;
  jobber_expense_enabled: boolean;
  jobber_expense_title_field_ref: string;
  jobber_expense_description_field_ref: string;
  jobber_expense_date_field_ref: string;
  jobber_expense_total_field_ref: string;
  google_drive_enabled: boolean;
  google_drive_client_id: string;
  google_drive_client_secret: string;
  google_drive_client_secret_configured?: boolean;
  google_drive_folder_id: string;
  firefly_enabled: boolean;
  firefly_instance_url: string;
  firefly_api_token: string;
  firefly_api_token_configured?: boolean;
  firefly_default_source_account: string;
  firefly_default_destination_account: string;
  firefly_default_currency: string;
  firefly_default_category: string;
  firefly_default_budget: string;
  firefly_notes_template: string;
  firefly_description_field_ref: string;
  firefly_date_field_ref: string;
  firefly_amount_field_ref: string;
  firefly_currency_field_ref: string;
  firefly_category_field_ref: string;
  firefly_budget_field_ref: string;
  firefly_notes_field_ref: string;
  firefly_external_ref_field_ref: string;
  firefly_source_account_field_ref: string;
  firefly_destination_account_field_ref: string;
  integration_public_url: string;
  paperless_webhook_secret: string;
}

interface WebhookStatus {
  paperless_configured?: boolean;
}

interface IntegrationStatus {
  provider: string;
  configured: boolean;
  connected: boolean;
  account_name?: string;
  account_id?: string;
  reason?: string;
}

const defaultSettings: SettingsData = {
  jobber_enabled: false,
  jobber_client_id: '',
  jobber_client_secret: '',
  jobber_client_secret_configured: false,
  jobber_job_id_field_id: 0,
  jobber_job_number_field_id: 0,
  jobber_client_field_id: 0,
  jobber_job_name_field_id: 0,
  jobber_expense_enabled: false,
  jobber_expense_title_field_ref: '',
  jobber_expense_description_field_ref: '',
  jobber_expense_date_field_ref: '',
  jobber_expense_total_field_ref: '',
  google_drive_enabled: false,
  google_drive_client_id: '',
  google_drive_client_secret: '',
  google_drive_client_secret_configured: false,
  google_drive_folder_id: '',
  firefly_enabled: false,
  firefly_instance_url: '',
  firefly_api_token: '',
  firefly_api_token_configured: false,
  firefly_default_source_account: '',
  firefly_default_destination_account: '',
  firefly_default_currency: 'USD',
  firefly_default_category: '',
  firefly_default_budget: '',
  firefly_notes_template: '',
  firefly_description_field_ref: '',
  firefly_date_field_ref: '',
  firefly_amount_field_ref: '',
  firefly_currency_field_ref: '',
  firefly_category_field_ref: '',
  firefly_budget_field_ref: '',
  firefly_notes_field_ref: '',
  firefly_external_ref_field_ref: '',
  firefly_source_account_field_ref: '',
  firefly_destination_account_field_ref: '',
  integration_public_url: '',
  paperless_webhook_secret: '',
};

const CUSTOM_FIELD_REF_PREFIX = 'custom_field:';

const builtInPaperlessFieldOptions: FieldOption[] = [
  { value: 'document.title', label: 'Document title' },
  { value: 'document.content', label: 'Document content' },
  { value: 'document.correspondent', label: 'Correspondent' },
  { value: 'document.created_date', label: 'Created date' },
  { value: 'document.document_type', label: 'Document type' },
  { value: 'document.original_file_name', label: 'Original filename' },
  { value: 'document.archived_file_name', label: 'Archived filename' },
];

function normalizeFieldRef(fieldRef: unknown, legacyFieldId: unknown): string {
  if (typeof fieldRef === 'string') {
    return fieldRef;
  }

  const parsedFieldId = Number(legacyFieldId || 0);
  if (parsedFieldId > 0) {
    return `${CUSTOM_FIELD_REF_PREFIX}${parsedFieldId}`;
  }

  return '';
}

function extractCustomFieldId(fieldRef: string): number {
  if (!fieldRef.startsWith(CUSTOM_FIELD_REF_PREFIX)) {
    return 0;
  }

  const parsed = Number(fieldRef.slice(CUSTOM_FIELD_REF_PREFIX.length));
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

const IntegrationsEditor: React.FC = () => {
  const [settings, setSettings] = useState<SettingsData>(defaultSettings);
  const [initialSettings, setInitialSettings] = useState<SettingsData>(defaultSettings);
  const [customFields, setCustomFields] = useState<CustomField[]>([]);
  const [statuses, setStatuses] = useState<Record<string, IntegrationStatus>>({});
  const [webhookStatus, setWebhookStatus] = useState<WebhookStatus>({});
  const [isDirty, setIsDirty] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [connectingProvider, setConnectingProvider] = useState<string | null>(null);
  const [disconnectingProvider, setDisconnectingProvider] = useState<string | null>(null);

  const hasOAuthCredentials = useCallback((clientId: string, clientSecret: string, clientSecretConfigured: boolean) => {
    return clientId.trim() !== '' && (clientSecret.trim() !== '' || clientSecretConfigured);
  }, []);

  const refreshData = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const [settingsRes, statusesRes] = await Promise.all([
        fetch('./api/settings'),
        fetch('./api/integrations'),
      ]);

      if (!settingsRes.ok) {
        throw new Error('Failed to fetch settings');
      }
      if (!statusesRes.ok) {
        throw new Error('Failed to fetch integrations');
      }

      const settingsData = await settingsRes.json();
      const integrationsData = await statusesRes.json();

      const nextSettings: SettingsData = {
        jobber_enabled: !!settingsData.settings?.jobber_enabled,
        jobber_client_id: settingsData.settings?.jobber_client_id || '',
        jobber_client_secret: '',
        jobber_client_secret_configured: !!settingsData.settings?.jobber_client_secret_configured,
        jobber_job_id_field_id: Number(settingsData.settings?.jobber_job_id_field_id || 0),
        jobber_job_number_field_id: Number(settingsData.settings?.jobber_job_number_field_id || 0),
        jobber_client_field_id: Number(settingsData.settings?.jobber_client_field_id || 0),
        jobber_job_name_field_id: Number(settingsData.settings?.jobber_job_name_field_id || 0),
        jobber_expense_enabled: !!settingsData.settings?.jobber_expense_enabled,
        jobber_expense_title_field_ref: normalizeFieldRef(
          settingsData.settings?.jobber_expense_title_field_ref,
          settingsData.settings?.jobber_expense_title_field_id,
        ),
        jobber_expense_description_field_ref: normalizeFieldRef(
          settingsData.settings?.jobber_expense_description_field_ref,
          settingsData.settings?.jobber_expense_description_field_id,
        ),
        jobber_expense_date_field_ref: normalizeFieldRef(
          settingsData.settings?.jobber_expense_date_field_ref,
          settingsData.settings?.jobber_expense_date_field_id,
        ),
        jobber_expense_total_field_ref: normalizeFieldRef(
          settingsData.settings?.jobber_expense_total_field_ref,
          settingsData.settings?.jobber_expense_total_field_id,
        ),
        google_drive_enabled: !!settingsData.settings?.google_drive_enabled,
        google_drive_client_id: settingsData.settings?.google_drive_client_id || '',
        google_drive_client_secret: '',
        google_drive_client_secret_configured: !!settingsData.settings?.google_drive_client_secret_configured,
        google_drive_folder_id: settingsData.settings?.google_drive_folder_id || '',
        firefly_enabled: !!settingsData.settings?.firefly_enabled,
        firefly_instance_url: settingsData.settings?.firefly_instance_url || '',
        firefly_api_token: '',
        firefly_api_token_configured: !!settingsData.settings?.firefly_api_token_configured,
        firefly_default_source_account: settingsData.settings?.firefly_default_source_account || '',
        firefly_default_destination_account: settingsData.settings?.firefly_default_destination_account || '',
        firefly_default_currency: settingsData.settings?.firefly_default_currency || 'USD',
        firefly_default_category: settingsData.settings?.firefly_default_category || '',
        firefly_default_budget: settingsData.settings?.firefly_default_budget || '',
        firefly_notes_template: settingsData.settings?.firefly_notes_template || '',
        firefly_description_field_ref: settingsData.settings?.firefly_description_field_ref || '',
        firefly_date_field_ref: settingsData.settings?.firefly_date_field_ref || 'document.created_date',
        firefly_amount_field_ref: settingsData.settings?.firefly_amount_field_ref || '',
        firefly_currency_field_ref: settingsData.settings?.firefly_currency_field_ref || '',
        firefly_category_field_ref: settingsData.settings?.firefly_category_field_ref || '',
        firefly_budget_field_ref: settingsData.settings?.firefly_budget_field_ref || '',
        firefly_notes_field_ref: settingsData.settings?.firefly_notes_field_ref || '',
        firefly_external_ref_field_ref: settingsData.settings?.firefly_external_ref_field_ref || '',
        firefly_source_account_field_ref: settingsData.settings?.firefly_source_account_field_ref || '',
        firefly_destination_account_field_ref: settingsData.settings?.firefly_destination_account_field_ref || '',
        integration_public_url: settingsData.settings?.integration_public_url || '',
        paperless_webhook_secret: '',
      };

      setSettings(nextSettings);
      setInitialSettings(nextSettings);
      setCustomFields(settingsData.custom_fields || []);
      setWebhookStatus(settingsData.webhooks || {});

      const statusMap: Record<string, IntegrationStatus> = {};
      for (const status of integrationsData.providers || integrationsData.integrations || []) {
        statusMap[status.provider] = status;
      }
      setStatuses(statusMap);
    } catch (err) {
      console.error('Error fetching integration data:', err);
      setError(err instanceof Error ? err.message : 'An unknown error occurred');
    } finally {
      setIsLoading(false);
    }
  }, []);

  const refreshStatuses = useCallback(async () => {
    const response = await fetch('./api/integrations');
    if (!response.ok) {
      throw new Error('Failed to fetch integrations');
    }
    const integrationsData = await response.json();
    const statusMap: Record<string, IntegrationStatus> = {};
    for (const status of integrationsData.providers || integrationsData.integrations || []) {
      statusMap[status.provider] = status;
    }
    setStatuses(statusMap);
  }, []);

  useEffect(() => {
    refreshData();
  }, [refreshData]);

  useEffect(() => {
    setIsDirty(JSON.stringify(settings) !== JSON.stringify(initialSettings));
  }, [settings, initialSettings]);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const integration = params.get('integration');
    const status = params.get('status');
    const result = params.get('result');
    if (!integration) {
      return;
    }

    if (status === 'connected' && result === 'ok') {
      setSuccessMessage(`${prettyProviderName(integration)} connected successfully.`);
      refreshData();
    } else if (status === 'error') {
      setError(`${prettyProviderName(integration)} connection failed. Please verify your server-side app credentials, redirect URL, and scopes.`);
    }

    params.delete('integration');
    params.delete('status');
    params.delete('result');
    const nextSearch = params.toString();
    const nextUrl = `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ''}`;
    window.history.replaceState({}, '', nextUrl);
  }, [refreshData]);

  const handleSettingChange = <K extends keyof SettingsData>(key: K, value: SettingsData[K]) => {
    setSettings((prev) => ({ ...prev, [key]: value }));
  };

  const persistSettings = useCallback(async (showSuccessMessage = true): Promise<boolean> => {
    if (!isDirty) {
      return true;
    }
    setIsSaving(true);
    setError(null);
    try {
      const payload = {
        ...settings,
        jobber_expense_title_field_id: extractCustomFieldId(settings.jobber_expense_title_field_ref),
        jobber_expense_description_field_id: extractCustomFieldId(settings.jobber_expense_description_field_ref),
        jobber_expense_date_field_id: extractCustomFieldId(settings.jobber_expense_date_field_ref),
        jobber_expense_total_field_id: extractCustomFieldId(settings.jobber_expense_total_field_ref),
      };
      const response = await fetch('./api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!response.ok) {
        const errData = await response.json();
        throw new Error(errData.error || 'Failed to save settings');
      }
      if (showSuccessMessage) {
        setSuccessMessage('Integration settings saved successfully!');
      }
      const savedSettings: SettingsData = {
        ...settings,
        jobber_client_secret: '',
        jobber_client_secret_configured: settings.jobber_client_secret.trim() !== '' || !!settings.jobber_client_secret_configured,
        google_drive_client_secret: '',
        google_drive_client_secret_configured: settings.google_drive_client_secret.trim() !== '' || !!settings.google_drive_client_secret_configured,
        firefly_api_token: '',
        firefly_api_token_configured: settings.firefly_api_token.trim() !== '' || !!settings.firefly_api_token_configured,
        paperless_webhook_secret: '',
      };
      if (settings.paperless_webhook_secret.trim() !== '') {
        setWebhookStatus((prev) => ({ ...prev, paperless_configured: true }));
      }
      setSettings(savedSettings);
      setInitialSettings(savedSettings);
      await refreshStatuses();
      if (showSuccessMessage) {
        setTimeout(() => setSuccessMessage(null), 3000);
      }
      return true;
    } catch (err) {
      console.error('Error saving integration settings:', err);
      setError(err instanceof Error ? err.message : 'An unknown error occurred');
      return false;
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, refreshStatuses, settings]);

  const handleSaveSettings = useCallback(async () => {
    await persistSettings(true);
  }, [persistSettings]);

  const handleConnect = useCallback(async (provider: string) => {
    setConnectingProvider(provider);
    setError(null);

    // Open the popup immediately within the user-gesture context so browsers
    // do not treat it as a pop-up blocker candidate.  We navigate it to the
    // real auth URL once we receive it from the server.
    const popup = window.open('about:blank', '_blank', 'width=640,height=800');
    if (!popup) {
      setError('Popup blocked. Please allow popups and try again.');
      setConnectingProvider(null);
      return;
    }

    let pollInterval: ReturnType<typeof setInterval> | null = null;

    const cleanup = () => {
      if (pollInterval !== null) {
        clearInterval(pollInterval);
        pollInterval = null;
      }
      window.removeEventListener('message', onMessage);
    };

    const onMessage = (event: MessageEvent) => {
      // Only accept messages from our own origin to prevent cross-origin injection.
      if (event.origin !== window.location.origin) return;
      if (!event.data || typeof event.data !== 'object') return;
      const { type, error } = event.data as { type?: string; error?: string };
      if (type === 'oauth_success') {
        cleanup();
        setSuccessMessage(`${prettyProviderName(provider)} connected successfully.`);
        refreshData();
      } else if (type === 'oauth_error') {
        cleanup();
        setError(`${prettyProviderName(provider)} connection failed: ${error || 'unknown error'}`);
        refreshData();
      }
    };

    window.addEventListener('message', onMessage);

    try {
      if (isDirty) {
        const saved = await persistSettings(false);
        if (!saved) {
          popup.close();
          cleanup();
          return;
        }
      }

      const response = await fetch(`./api/integrations/${provider}/connect/start`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ return_path: '/settings' }),
      });
      const payload = await parseJsonResponse<{ error?: string; redirect_url?: string; url?: string }>(response);
      if (!response.ok) {
        popup.close();
        cleanup();
        throw new Error(payload.error || 'Failed to start connection');
      }

      const authURL = payload.redirect_url || payload.url;
      if (!authURL) {
        popup.close();
        cleanup();
        throw new Error('No redirect URL returned from server');
      }

      // Navigate the already-open popup to the OAuth authorization page.
      popup.location.href = authURL;

      // Also poll for popup close as a fallback (postMessage may be blocked by
      // the browser if the callback page's origin differs).
      pollInterval = setInterval(() => {
        try {
          if (popup.closed) {
            cleanup();
            refreshData();
          }
        } catch {
          // Ignore cross-origin access errors during polling.
          cleanup();
          refreshData();
        }
      }, 500);
    } catch (err) {
      console.error(`Error connecting ${provider}:`, err);
      setError(err instanceof Error ? err.message : 'An unknown error occurred');
    } finally {
      setConnectingProvider(null);
    }
  }, [isDirty, persistSettings, refreshData]);

  const handleDisconnect = useCallback(async (provider: string) => {
    setDisconnectingProvider(provider);
    setError(null);
    try {
      const response = await fetch(`./api/integrations/${provider}/disconnect`, {
        method: 'POST',
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || 'Failed to disconnect provider');
      }
      setSuccessMessage(`${prettyProviderName(provider)} disconnected.`);
      setTimeout(() => setSuccessMessage(null), 3000);
      await refreshData();
    } catch (err) {
      console.error(`Error disconnecting ${provider}:`, err);
      setError(err instanceof Error ? err.message : 'An unknown error occurred');
    } finally {
      setDisconnectingProvider(null);
    }
  }, [refreshData]);

  const customFieldOptions = useMemo(
    () => [{ id: 0, name: 'Not mapped' }, ...customFields.map((field) => ({ id: field.id, name: field.name }))],
    [customFields],
  );

  const expenseFieldOptions = useMemo(
    () => [
      { value: '', label: 'Use default behavior' },
      ...builtInPaperlessFieldOptions,
      ...customFields.map((field) => ({
        value: `${CUSTOM_FIELD_REF_PREFIX}${field.id}`,
        label: `Custom field: ${field.name}`,
      })),
    ],
    [customFields],
  );

  const oauthReady = {
    jobber: hasOAuthCredentials(
      settings.jobber_client_id,
      settings.jobber_client_secret,
      !!settings.jobber_client_secret_configured,
    ),
    google_drive: hasOAuthCredentials(
      settings.google_drive_client_id,
      settings.google_drive_client_secret,
      !!settings.google_drive_client_secret_configured,
    ),
  };

  if (isLoading) {
    return <div className="p-6">Loading...</div>;
  }

  return (
    <div className="p-6 bg-gray-100 dark:bg-gray-900">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-3xl font-bold text-gray-800 dark:text-gray-200">Integrations</h1>
      </div>

      {successMessage && (
        <div className="fixed bottom-4 right-4 bg-green-500 text-white px-6 py-3 rounded-lg shadow-lg transition-transform transform animate-bounce" role="alert">
          <span className="block sm:inline">{successMessage}</span>
        </div>
      )}

      {error && (
        <div className="mb-4 bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded relative" role="alert">
          <span className="block sm:inline">{error}</span>
        </div>
      )}

      <div className="grid grid-cols-1 gap-6">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-5">
          <SectionHeader
            title="OAuth callback URL"
            description="Set this when Paperless GPT is behind a reverse proxy or accessed through a public domain. Register each provider callback URL using this base URL."
          />
          <TextInput
            label="Public Paperless GPT URL"
            value={settings.integration_public_url}
            onChange={(v) => handleSettingChange('integration_public_url', v)}
            placeholder={window.location.origin}
          />
          <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
            Example callback: {(settings.integration_public_url || window.location.origin).replace(/\/$/, '')}/api/integrations/jobber/oauth/callback
          </p>
          <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
            Google Drive callback: {(settings.integration_public_url || window.location.origin).replace(/\/$/, '')}/api/integrations/google_drive/oauth/callback
          </p>
        </div>

        <IntegrationCard
          title="Jobber"
          status={statuses.jobber}
          canConnect={oauthReady.jobber}
          connectHelpText="Enter a Jobber client ID and client secret to connect."
          onConnect={() => handleConnect('jobber')}
          onDisconnect={() => handleDisconnect('jobber')}
          connecting={connectingProvider === 'jobber'}
          disconnecting={disconnectingProvider === 'jobber'}
        >
          <OAuthCredentialFields
            providerName="Jobber"
            clientId={settings.jobber_client_id}
            clientSecret={settings.jobber_client_secret}
            clientSecretConfigured={!!settings.jobber_client_secret_configured}
            onClientIdChange={(value) => handleSettingChange('jobber_client_id', value)}
            onClientSecretChange={(value) => handleSettingChange('jobber_client_secret', value)}
          />

          {/* ── Job matching ───────────────────────────────────────────── */}
          <SectionHeader
            title="Job matching"
            description="When you approve a document, the selected Jobber job's details are written back into Paperless-ngx custom fields. Map each Jobber value to the Paperless custom field where you want it stored."
          />

          <div className="flex items-center gap-2 mb-4">
            <input
              type="checkbox"
              id="jobberEnabled"
              checked={settings.jobber_enabled}
              onChange={(e) => handleSettingChange('jobber_enabled', e.target.checked)}
              className="w-4 h-4"
            />
            <label htmlFor="jobberEnabled" className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Enable Jobber job matching
            </label>
          </div>

          <CustomFieldsInfoBanner customFields={customFields} />

          <fieldset disabled={!settings.jobber_enabled} className="grid grid-cols-1 md:grid-cols-2 gap-4 disabled:opacity-50 mt-2">
            <FieldMappingSelect
              label="Job ID"
              tooltip="The internal Jobber job ID. Useful for cross-referencing or automations that need a stable identifier."
              value={settings.jobber_job_id_field_id}
              options={customFieldOptions}
              onChange={(value) => handleSettingChange('jobber_job_id_field_id', value)}
            />
            <FieldMappingSelect
              label="Job number"
              tooltip="The human-readable job number shown in Jobber (e.g. #1107). Handy for quick visual lookup."
              value={settings.jobber_job_number_field_id}
              options={customFieldOptions}
              onChange={(value) => handleSettingChange('jobber_job_number_field_id', value)}
            />
            <FieldMappingSelect
              label="Client name"
              tooltip="The client or company name associated with the Jobber job."
              value={settings.jobber_client_field_id}
              options={customFieldOptions}
              onChange={(value) => handleSettingChange('jobber_client_field_id', value)}
            />
            <FieldMappingSelect
              label="Job name"
              tooltip="The title of the Jobber job (e.g. 'Bathroom renovation – Phase 2')."
              value={settings.jobber_job_name_field_id}
              options={customFieldOptions}
              onChange={(value) => handleSettingChange('jobber_job_name_field_id', value)}
            />
          </fieldset>

          {/* ── Expense creation ───────────────────────────────────────── */}
          <div className="mt-6 border-t border-gray-200 dark:border-gray-700 pt-5">
            <SectionHeader
              title="Expense creation"
              description="When a document is approved with a Jobber job selected, an expense can be automatically created in Jobber. Choose which Paperless field to use as the source for each expense field."
            />

            <div className="flex items-center gap-2 mb-4">
              <input
                type="checkbox"
                id="jobberExpenseEnabled"
                checked={settings.jobber_expense_enabled}
                onChange={(e) => handleSettingChange('jobber_expense_enabled', e.target.checked)}
                className="w-4 h-4"
              />
              <label htmlFor="jobberExpenseEnabled" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Enable expense creation
              </label>
            </div>

            <fieldset
              disabled={!settings.jobber_enabled || !settings.jobber_expense_enabled}
              className="grid grid-cols-1 md:grid-cols-2 gap-4 disabled:opacity-50"
            >
              <FieldReferenceSelect
                label="Expense title"
                tooltip="The name shown on the expense in Jobber. Defaults to the document title when not mapped."
                value={settings.jobber_expense_title_field_ref}
                options={expenseFieldOptions}
                customFields={customFields}
                onChange={(value) => handleSettingChange('jobber_expense_title_field_ref', value)}
              />
              <FieldReferenceSelect
                label="Expense description"
                tooltip="Optional longer note on the expense. Defaults to a summary of the vendor and document type."
                value={settings.jobber_expense_description_field_ref}
                options={expenseFieldOptions}
                customFields={customFields}
                onChange={(value) => handleSettingChange('jobber_expense_description_field_ref', value)}
              />
              <FieldReferenceSelect
                label="Expense date"
                tooltip="The date of the expense in Jobber. Defaults to the document's created date."
                value={settings.jobber_expense_date_field_ref}
                options={expenseFieldOptions}
                customFields={customFields}
                onChange={(value) => handleSettingChange('jobber_expense_date_field_ref', value)}
              />
              <FieldReferenceSelect
                label="Expense total"
                tooltip="The monetary amount of the expense. Currency symbols and commas are stripped automatically. Defaults to scanning custom fields whose name contains 'total' or 'amount'."
                value={settings.jobber_expense_total_field_ref}
                options={expenseFieldOptions}
                customFields={customFields}
                onChange={(value) => handleSettingChange('jobber_expense_total_field_ref', value)}
              />
            </fieldset>
          </div>
        </IntegrationCard>

        <IntegrationCard
          title="Google Drive"
          status={statuses.google_drive}
          canConnect={oauthReady.google_drive}
          connectHelpText="Enter a Google Drive client ID and client secret to connect."
          onConnect={() => handleConnect('google_drive')}
          onDisconnect={() => handleDisconnect('google_drive')}
          connecting={connectingProvider === 'google_drive'}
          disconnecting={disconnectingProvider === 'google_drive'}
        >
          <OAuthCredentialFields
            providerName="Google Drive"
            clientId={settings.google_drive_client_id}
            clientSecret={settings.google_drive_client_secret}
            clientSecretConfigured={!!settings.google_drive_client_secret_configured}
            onClientIdChange={(value) => handleSettingChange('google_drive_client_id', value)}
            onClientSecretChange={(value) => handleSettingChange('google_drive_client_secret', value)}
          />

          <div className="flex items-center mb-4">
            <input
              type="checkbox"
              id="googleDriveEnabled"
              checked={settings.google_drive_enabled}
              onChange={(e) => handleSettingChange('google_drive_enabled', e.target.checked)}
              className="w-4 h-4 mr-2"
            />
            <label htmlFor="googleDriveEnabled">Enable Google Drive uploads</label>
          </div>

          <fieldset disabled={!settings.google_drive_enabled} className="disabled:opacity-50">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Google Drive folder ID
            </label>
            <input
              type="text"
              value={settings.google_drive_folder_id}
              onChange={(e) => handleSettingChange('google_drive_folder_id', e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded px-3 py-2 mt-2 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-gray-200"
              placeholder="Folder ID"
            />
            <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
              Uploaded files will use the latest filename from Paperless-ngx.
            </p>
          </fieldset>
        </IntegrationCard>

        <IntegrationCard
          title="Firefly III"
          status={statuses.firefly}
          onConnect={() => undefined}
          onDisconnect={() => undefined}
          connecting={false}
          disconnecting={false}
          showOAuthActions={false}
        >
          <SectionHeader
            title="Personal Access Token"
            description="Firefly uses a Personal Access Token and never OAuth. The token is encrypted at rest and only shown as configured after saving."
          />
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
              <span className="block">Instance URL</span>
              <input className="mt-1.5 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200" value={settings.firefly_instance_url} onChange={(e) => handleSettingChange('firefly_instance_url', e.target.value)} placeholder="https://firefly.example.com" />
            </label>
            <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
              <span className="block">Personal Access Token {settings.firefly_api_token_configured ? '(configured)' : ''}</span>
              <input type="password" className="mt-1.5 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200" value={settings.firefly_api_token} onChange={(e) => handleSettingChange('firefly_api_token', e.target.value)} placeholder={settings.firefly_api_token_configured ? 'Leave blank to keep existing token' : 'Firefly PAT'} />
            </label>
          </div>
          <div className="mt-4 flex items-center gap-2">
            <input type="checkbox" id="fireflyEnabled" checked={settings.firefly_enabled} onChange={(e) => handleSettingChange('firefly_enabled', e.target.checked)} className="w-4 h-4" />
            <label htmlFor="fireflyEnabled" className="text-sm font-medium text-gray-700 dark:text-gray-300">Enable Firefly match/create/attach</label>
          </div>
          <div className="mt-5 grid grid-cols-1 md:grid-cols-2 gap-4">
            <TextInput label="Default source account" value={settings.firefly_default_source_account} onChange={(v) => handleSettingChange('firefly_default_source_account', v)} />
            <TextInput label="Default destination account" value={settings.firefly_default_destination_account} onChange={(v) => handleSettingChange('firefly_default_destination_account', v)} />
            <TextInput label="Default currency" value={settings.firefly_default_currency} onChange={(v) => handleSettingChange('firefly_default_currency', v)} />
            <TextInput label="Default category" value={settings.firefly_default_category} onChange={(v) => handleSettingChange('firefly_default_category', v)} />
            <TextInput label="Default budget" value={settings.firefly_default_budget} onChange={(v) => handleSettingChange('firefly_default_budget', v)} />
            <TextInput label="Notes template" value={settings.firefly_notes_template} onChange={(v) => handleSettingChange('firefly_notes_template', v)} />
          </div>
          <div className="mt-5">
            <SectionHeader title="Field mappings" description="Choose where Firefly transaction fields come from. Amount is required; if unmapped, Paperless GPT scans suggested custom fields named total, amount, or price." />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <FieldReferenceSelect label="Description" tooltip="Transaction description." value={settings.firefly_description_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_description_field_ref', value)} />
              <FieldReferenceSelect label="Date" tooltip="Transaction date." value={settings.firefly_date_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_date_field_ref', value)} />
              <FieldReferenceSelect label="Amount" tooltip="Required transaction amount." value={settings.firefly_amount_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_amount_field_ref', value)} />
              <FieldReferenceSelect label="Currency" tooltip="Currency code." value={settings.firefly_currency_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_currency_field_ref', value)} />
              <FieldReferenceSelect label="Category" tooltip="Optional category override." value={settings.firefly_category_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_category_field_ref', value)} />
              <FieldReferenceSelect label="Budget" tooltip="Optional budget override." value={settings.firefly_budget_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_budget_field_ref', value)} />
              <FieldReferenceSelect label="Notes" tooltip="Optional notes field." value={settings.firefly_notes_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_notes_field_ref', value)} />
              <FieldReferenceSelect label="External reference" tooltip="Optional external reference." value={settings.firefly_external_ref_field_ref} options={expenseFieldOptions} customFields={customFields} onChange={(value) => handleSettingChange('firefly_external_ref_field_ref', value)} />
            </div>
          </div>
        </IntegrationCard>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-5">
          <SectionHeader
            title="Paperless webhook"
            description="Optional: configure Paperless-ngx to POST document.created and document.updated events here so Paperless GPT can pre-generate suggestions before you open the review queue. If this is not configured, the existing polling fallback remains active."
          />
          <p className="mb-4 text-sm text-gray-600 dark:text-gray-300">
            Webhook URL: <code>{window.location.origin}/api/paperless/webhook</code>
          </p>
          <p className="mb-4 text-sm text-gray-600 dark:text-gray-300">
            Status: {webhookStatus.paperless_configured ? 'secret configured' : 'not configured'}
          </p>
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
            Shared secret
          </label>
          <input
            type="password"
            value={settings.paperless_webhook_secret}
            onChange={(e) => handleSettingChange('paperless_webhook_secret', e.target.value)}
            className="mt-2 w-full rounded border border-gray-300 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
            placeholder={webhookStatus.paperless_configured ? 'Leave blank to keep existing secret' : 'Enter shared webhook secret'}
          />
          <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
            The secret is stored separately from settings.json and is never shown after saving.
          </p>
        </div>
      </div>

      <div className="flex justify-end mt-6">
        <button
          onClick={handleSaveSettings}
          disabled={!isDirty || isSaving}
          aria-busy={isSaving}
          className={`px-6 py-2 rounded-md font-semibold focus:outline-none focus:ring-2 focus:ring-offset-2 transition-transform transform ${
            isSaving
              ? 'bg-blue-400 text-white cursor-not-allowed'
              : 'bg-blue-600 text-white hover:bg-blue-700 hover:scale-105 focus:ring-blue-500'
          } ${!isDirty && !isSaving ? 'disabled:bg-gray-400 disabled:cursor-not-allowed' : ''}`}
        >
          {isSaving ? 'Saving…' : 'Save Changes'}
        </button>
      </div>

      <div className="mt-8">
        <IntegrationActionLog />
      </div>
    </div>
  );
};

interface IntegrationCardProps {
  title: string;
  status?: IntegrationStatus;
  canConnect?: boolean;
  connectHelpText?: string;
  onConnect: () => void;
  onDisconnect: () => void;
  connecting: boolean;
  disconnecting: boolean;
  showOAuthActions?: boolean;
  children: React.ReactNode;
}

const IntegrationCard: React.FC<IntegrationCardProps> = ({
  title,
  status,
  canConnect,
  connectHelpText,
  onConnect,
  onDisconnect,
  connecting,
  disconnecting,
  showOAuthActions = true,
  children,
}) => {
  const connectReady = canConnect ?? !!status?.configured;

  return (
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-5">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 mb-4">
        <div>
          <h2 className="text-xl font-semibold text-gray-700 dark:text-gray-300">{title}</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {status?.configured ? (
              status.connected ? (
                <>
                  Connected{status.account_name ? ` as ${status.account_name}` : ''}.
                </>
              ) : (
                'Configured but not connected.'
              )
            ) : (
              status?.reason || 'Provider is not configured.'
            )}
          </p>
        </div>

        {showOAuthActions && (
          <div className="flex flex-col items-start gap-1 md:items-end">
            <div className="flex gap-2">
              <button
                onClick={onConnect}
                disabled={!connectReady || connecting}
                title={!connectReady ? connectHelpText || status?.reason || 'Provider is not configured.' : undefined}
                className="px-4 py-2 rounded bg-blue-600 text-white hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed"
              >
                {connecting ? 'Connecting…' : 'Connect'}
              </button>
              <button
                onClick={onDisconnect}
                disabled={!status?.connected || disconnecting}
                className="px-4 py-2 rounded bg-gray-200 text-gray-800 hover:bg-gray-300 disabled:bg-gray-100 disabled:text-gray-400 disabled:cursor-not-allowed dark:bg-gray-700 dark:text-gray-200 dark:hover:bg-gray-600"
              >
                {disconnecting ? 'Disconnecting…' : 'Disconnect'}
              </button>
            </div>
            {!connectReady && (
              <p className="max-w-xs text-xs text-gray-500 dark:text-gray-400">
                {connectHelpText || status?.reason || 'Provider is not configured.'}
              </p>
            )}
          </div>
        )}
      </div>

      {children}
    </div>
  );
};

// ── Tooltip ────────────────────────────────────────────────────────────────

interface TooltipProps {
  text: string;
}

const Tooltip: React.FC<TooltipProps> = ({ text }) => {
  const [visible, setVisible] = useState(false);
  const ref = useRef<HTMLSpanElement>(null);

  return (
    <span className="relative inline-flex items-center ml-1.5" ref={ref}>
      <button
        type="button"
        aria-label="More information"
        onMouseEnter={() => setVisible(true)}
        onMouseLeave={() => setVisible(false)}
        onFocus={() => setVisible(true)}
        onBlur={() => setVisible(false)}
        className="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 focus:outline-none"
      >
        <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-3a1 1 0 00-.867.5 1 1 0 11-1.731-1A3 3 0 0113 8a3.001 3.001 0 01-2 2.83V11a1 1 0 11-2 0v-1a1 1 0 011-1 1 1 0 100-2zm0 8a1 1 0 100-2 1 1 0 000 2z" clipRule="evenodd" />
        </svg>
      </button>
      {visible && (
        <span
          role="tooltip"
          className="absolute z-20 bottom-full left-1/2 -translate-x-1/2 mb-2 w-56 rounded-md bg-gray-800 text-white text-xs leading-relaxed px-3 py-2 shadow-lg pointer-events-none"
        >
          {text}
          <span className="absolute top-full left-1/2 -translate-x-1/2 border-4 border-transparent border-t-gray-800" />
        </span>
      )}
    </span>
  );
};

// ── Section header ──────────────────────────────────────────────────────────

interface SectionHeaderProps {
  title: string;
  description: string;
}

const SectionHeader: React.FC<SectionHeaderProps> = ({ title, description }) => (
  <div className="mb-4">
    <h3 className="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{title}</h3>
    <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{description}</p>
  </div>
);

// ── Custom-fields prerequisite info banner ──────────────────────────────────

interface CustomFieldsInfoBannerProps {
  customFields: CustomField[];
}

const CustomFieldsInfoBanner: React.FC<CustomFieldsInfoBannerProps> = ({ customFields }) => {
  if (customFields.length > 0) return null;
  return (
    <div className="mb-4 flex gap-2.5 rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 text-sm text-amber-800 dark:border-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
      <svg className="mt-0.5 h-4 w-4 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <path fillRule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 5a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 5zm0 9a1 1 0 100-2 1 1 0 000 2z" clipRule="evenodd" />
      </svg>
      <span>
        No custom fields found in Paperless-ngx. To use field mapping, first create your custom fields in{' '}
        <strong>Paperless-ngx → Settings → Custom Fields</strong>, then reload this page.
      </span>
    </div>
  );
};

// ── FieldMappingSelect ──────────────────────────────────────────────────────

interface FieldMappingSelectProps {
  label: string;
  tooltip: string;
  value: number;
  options: Array<{ id: number; name: string }>;
  onChange: (value: number) => void;
}

const FieldMappingSelect: React.FC<FieldMappingSelectProps> = ({ label, tooltip, value, options, onChange }) => (
  <div>
    <label className="flex items-center text-sm font-medium text-gray-700 dark:text-gray-300">
      {label}
      <Tooltip text={tooltip} />
    </label>
    <p className="mt-0.5 text-xs text-gray-400 dark:text-gray-500">
      Jobber → Paperless custom field
    </p>
    <select
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      className="mt-1.5 w-full rounded border border-gray-300 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
    >
      {options.map((option) => (
        <option key={option.id} value={option.id}>
          {option.name}
        </option>
      ))}
    </select>
  </div>
);

// ── FieldReferenceSelect ────────────────────────────────────────────────────

interface FieldReferenceSelectProps {
  label: string;
  tooltip: string;
  value: string;
  options: FieldOption[];
  customFields: CustomField[];
  onChange: (value: string) => void;
}

const FieldReferenceSelect: React.FC<FieldReferenceSelectProps> = ({ label, tooltip, value, options, customFields, onChange }) => {
  const builtIn = options.filter((o) => o.value === '' || builtInPaperlessFieldOptions.some((b) => b.value === o.value));
  const custom = customFields.map((f) => ({ value: `${CUSTOM_FIELD_REF_PREFIX}${f.id}`, label: f.name }));

  return (
    <div>
      <label className="flex items-center text-sm font-medium text-gray-700 dark:text-gray-300">
        {label}
        <Tooltip text={tooltip} />
      </label>
      <p className="mt-0.5 text-xs text-gray-400 dark:text-gray-500">
        Paperless field → Jobber expense
      </p>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="mt-1.5 w-full rounded border border-gray-300 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
      >
        {builtIn.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
        {custom.length > 0 && (
          <optgroup label="Custom fields">
            {custom.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </optgroup>
        )}
      </select>
    </div>
  );
};

interface OAuthCredentialFieldsProps {
  providerName: string;
  clientId: string;
  clientSecret: string;
  clientSecretConfigured: boolean;
  onClientIdChange: (value: string) => void;
  onClientSecretChange: (value: string) => void;
}

const OAuthCredentialFields: React.FC<OAuthCredentialFieldsProps> = ({
  providerName,
  clientId,
  clientSecret,
  clientSecretConfigured,
  onClientIdChange,
  onClientSecretChange,
}) => (
  <div className="mb-5 border-b border-gray-200 pb-5 dark:border-gray-700">
    <SectionHeader
      title="Connection setup"
      description={`Enter your ${providerName} OAuth app credentials here, then save changes before connecting.`}
    />
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      <TextInput label="Client ID" value={clientId} onChange={onClientIdChange} />
      <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
        <span className="block">Client secret {clientSecretConfigured ? '(configured)' : ''}</span>
        <input
          type="password"
          value={clientSecret}
          onChange={(e) => onClientSecretChange(e.target.value)}
          className="mt-1.5 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
          placeholder={clientSecretConfigured ? 'Leave blank to keep existing secret' : 'Client secret'}
        />
      </label>
    </div>
  </div>
);

const TextSetting: React.FC<{ label: string; value: string; onChange: (value: string) => void; placeholder?: string }> = ({ label, value, onChange, placeholder }) => (
  <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
    <span className="block">{label}</span>
    <input
      type="text"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="mt-1.5 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
    />
  </label>
);

const TextInput = TextSetting;

function prettyProviderName(provider: string): string {
  switch (provider) {
    case 'google_drive':
      return 'Google Drive';
    case 'jobber':
      return 'Jobber';
    case 'firefly':
      return 'Firefly III';
    default:
      return provider;
  }
}

async function parseJsonResponse<T>(response: Response): Promise<T> {
  const text = await response.text();
  if (!text.trim()) {
    return {} as T;
  }
  return JSON.parse(text) as T;
}

export default IntegrationsEditor;

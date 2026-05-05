import React from 'react';
import PromptsEditor from './PromptsEditor';
import CustomFieldsEditor from './CustomFieldsEditor';
import MetadataRestrictionsEditor from './MetadataRestrictionsEditor';
import IntegrationsEditor from './IntegrationsEditor';
import ChangePassword from './ChangePassword';
import AIProvidersEditor from './AIProvidersEditor';
import ConnectorIntegrations from './ConnectorIntegrations';
import ExternalApiSettings from './ExternalApiSettings';

const Settings: React.FC = () => {
  return (
    <main className="mx-auto max-w-7xl px-6 py-8">
      <section className="mb-8 rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
        <p className="text-sm font-semibold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">
          Settings
        </p>
        <h1 className="mt-2 text-3xl font-bold text-gray-900 dark:text-gray-100">
          Paperless GPT configuration
        </h1>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-gray-600 dark:text-gray-300">
          Configure your AI provider, connect external apps such as Bricopro HQ, and
          tune prompts and metadata behaviour.
        </p>
      </section>

      <div className="space-y-8">
        <AIProvidersEditor />
        <ConnectorIntegrations />
        <PromptsEditor />
        <MetadataRestrictionsEditor />
        <CustomFieldsEditor />
        <ExternalApiSettings />
      </div>

      <div className="p-6 bg-gray-100 dark:bg-gray-900">
        <IntegrationsEditor />
      </div>

      <div className="mt-8">
        <ChangePassword />
      </div>
    </main>
  );
};

export default Settings;

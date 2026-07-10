import { HeartIcon } from '@heroicons/react/24/outline';
import axios from 'axios';
import React, { useEffect, useState } from 'react';
import PromptsEditor from './PromptsEditor';
import CustomFieldsEditor from './CustomFieldsEditor';

interface VersionInfo {
  version: string;
  commit: string;
  buildDate: string;
}

const SupportSection: React.FC = () => {
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);

  useEffect(() => {
    const fetchVersion = async () => {
      try {
        const response = await axios.get<VersionInfo>('./api/version');
        setVersionInfo(response.data);
      } catch (error) {
        console.error('Failed to fetch version information:', error);
      }
    };
    fetchVersion();
  }, []);

  return (
    <section
      aria-labelledby="support-heading"
      className="rounded-lg border border-line bg-surface p-6"
    >
      <h2 id="support-heading" className="flex items-center gap-2 text-lg font-semibold">
        <HeartIcon className="h-5 w-5 text-neg" aria-hidden="true" />
        Support paperless-gpt
      </h2>
      <p className="mt-2 max-w-prose text-sm text-muted">
        paperless-gpt is free, open source, and maintained in spare time. If it
        saves you hours of document chaos, consider fueling future development.
      </p>
      <a
        href="https://buymeacoffee.com/icereed"
        target="_blank"
        rel="noopener noreferrer"
        className="mt-4 inline-flex h-9 items-center rounded-md border border-line bg-surface px-4 text-sm font-medium transition-colors duration-150 ease-out-quart hover:bg-surface-2"
      >
        Buy the maintainer a coffee
      </a>
      {versionInfo && (
        <p className="mt-4 text-xs text-faint">
          paperless-gpt {versionInfo.version}
          {versionInfo.commit &&
            versionInfo.commit !== 'devCommit' &&
            versionInfo.commit.length >= 7 && (
              <span> ({versionInfo.commit.slice(0, 7)})</span>
            )}
        </p>
      )}
    </section>
  );
};

const Settings: React.FC = () => {
  return (
    <div className="mx-auto max-w-5xl space-y-8 px-4 py-8 sm:px-6">
      <h1 className="text-xl font-semibold">Settings</h1>

      {/* The editors bring their own panels; no extra card around them. */}
      <section aria-label="Prompts">
        <PromptsEditor />
      </section>

      <section aria-label="Custom fields">
        <CustomFieldsEditor />
      </section>

      <SupportSection />
    </div>
  );
};

export default Settings;

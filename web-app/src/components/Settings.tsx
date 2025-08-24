import React, { useState, useEffect, useCallback } from 'react';
import PromptsEditor from './PromptsEditor';

interface CustomField {
  id: number;
  name: string;
  data_type: string;
}

interface SettingsData {
  selected_custom_field_ids: number[];
  custom_field_write_mode: 'append' | 'replace';
  auto_generate_custom_field: boolean;
}

const Settings: React.FC = () => {
  const [customFields, setCustomFields] = useState<CustomField[]>([]);
  const [settings, setSettings] = useState<SettingsData>({
    selected_custom_field_ids: [],
    custom_field_write_mode: 'append',
    auto_generate_custom_field: false,
  });
  const [initialSettings, setInitialSettings] = useState<SettingsData | null>(null);
  const [isDirty, setIsDirty] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // Fetch initial data
  useEffect(() => {
    const fetchInitialData = async () => {
      try {
        const [fieldsRes, settingsRes] = await Promise.all([
          fetch('/api/paperless/custom_fields'),
          fetch('/api/settings'),
        ]);

        if (!fieldsRes.ok) throw new Error('Failed to fetch custom fields');
        const fieldsData: CustomField[] = await fieldsRes.json();
        setCustomFields(fieldsData);

        if (!settingsRes.ok) throw new Error('Failed to fetch settings');
        const settingsData: SettingsData = await settingsRes.json();
        setSettings(settingsData);
        setInitialSettings(settingsData);
      } catch (err) {
        console.error('Error fetching initial data:', err);
        setError(err instanceof Error ? err.message : 'An unknown error occurred');
      }
    };
    fetchInitialData();
  }, []);

  // Check for changes to set dirty state
  useEffect(() => {
    if (initialSettings) {
      const hasChanged = JSON.stringify(settings) !== JSON.stringify(initialSettings);
      setIsDirty(hasChanged);
    }
  }, [settings, initialSettings]);

  const handleSaveSettings = useCallback(async () => {
    if (!isDirty) return;
    setIsSaving(true);
    setError(null);
    try {
      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      });
      if (!response.ok) {
        const errData = await response.json();
        throw new Error(errData.error || 'Failed to save settings');
      }
      // On success, update the initial state to match the current state
      setInitialSettings(settings);
      setSuccessMessage('Settings saved successfully!');
      setTimeout(() => setSuccessMessage(null), 3000);
    } catch (err) {
      console.error('Error saving settings:', err);
      setError(err instanceof Error ? err.message : 'An unknown error occurred');
      setTimeout(() => setError(null), 5000);
    } finally {
      setIsSaving(false);
    }
  }, [settings, isDirty]);

  const handleSettingChange = <K extends keyof SettingsData>(key: K, value: SettingsData[K]) => {
    setSettings((prev) => ({ ...prev, [key]: value }));
  };

  const handleFieldSelectionChange = (fieldId: number) => {
    const newSelectedIds = settings.selected_custom_field_ids.includes(fieldId)
      ? settings.selected_custom_field_ids.filter((id) => id !== fieldId)
      : [...settings.selected_custom_field_ids, fieldId];
    handleSettingChange('selected_custom_field_ids', newSelectedIds);
  };

  return (
    <main className="p-4">
      <h1 className="mb-4 text-2xl font-semibold">Settings</h1>
      
      {successMessage && (
        <div className="fixed bottom-4 right-4 bg-green-500 text-white px-6 py-3 rounded-lg shadow-lg transition-transform transform animate-bounce" role="alert">
          <span className="block sm:inline">{successMessage}</span>
        </div>
      )}
      
      {error && (
        <div className="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded relative mb-4" role="alert">
          <span className="block sm:inline">{error}</span>
        </div>
      )}

      <div className="p-6 bg-gray-100 dark:bg-gray-900">
        <PromptsEditor />
      </div>

      <div className="p-6 bg-gray-100 dark:bg-gray-900 mt-6">
        <h2 className="text-xl font-semibold mb-4 text-gray-700 dark:text-gray-300">Custom Fields</h2>
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
          <div className="flex justify-end items-center mb-4">
            <button
              onClick={handleSaveSettings}
              disabled={!isDirty || isSaving}
              className="px-4 py-2 rounded-md font-semibold text-white bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed"
            >
              {isSaving ? 'Saving...' : 'Save Settings'}
            </button>
          </div>
          <div className="flex items-center mb-4">
            <input
              type="checkbox"
              id="autoGenerateCustomFields"
              checked={settings.auto_generate_custom_field}
              onChange={(e) => handleSettingChange('auto_generate_custom_field', e.target.checked)}
              className="w-4 h-4 mr-2"
            />
            <label htmlFor="autoGenerateCustomFields">
              Automatically generate custom fields
            </label>
          </div>

          <fieldset disabled={!settings.auto_generate_custom_field} className="disabled:opacity-50">
            <div className="mb-4">
              <h3 className="mb-2 font-semibold">Fields to process:</h3>
              <div className="grid grid-cols-2 gap-2 md:grid-cols-3 lg:grid-cols-4">
                {customFields.map((field) => (
                  <div key={field.id} className="flex items-center">
                    <input
                      type="checkbox"
                      id={`field-${field.id}`}
                      checked={settings.selected_custom_field_ids.includes(field.id)}
                      onChange={() => handleFieldSelectionChange(field.id)}
                      className="w-4 h-4 mr-2"
                    />
                    <label htmlFor={`field-${field.id}`}>{field.name}</label>
                  </div>
                ))}
              </div>
            </div>

            <div>
              <h3 className="mb-2 font-semibold">Write Mode:</h3>
              <div className="flex items-center mb-2">
                <input
                  type="radio"
                  id="writeModeAppend"
                  name="writeMode"
                  value="append"
                  checked={settings.custom_field_write_mode === 'append'}
                  onChange={() => handleSettingChange('custom_field_write_mode', 'append')}
                  className="w-4 h-4 mr-2"
                />
                <label htmlFor="writeModeAppend">
                  Append (only fill empty fields)
                </label>
              </div>
              <div className="flex items-center">
                <input
                  type="radio"
                  id="writeModeReplace"
                  name="writeMode"
                  value="replace"
                  checked={settings.custom_field_write_mode === 'replace'}
                  onChange={() => handleSettingChange('custom_field_write_mode', 'replace')}
                  className="w-4 h-4 mr-2"
                />
                <label htmlFor="writeModeReplace">
                  Replace (overwrite existing fields)
                </label>
              </div>
            </div>
          </fieldset>
        </div>
      </div>
    </main>
  );
};

export default Settings;

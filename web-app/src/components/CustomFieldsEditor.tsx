import React, { useState, useEffect, useCallback } from 'react';

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

const CustomFieldsEditor: React.FC = () => {
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
    <div className="p-6 bg-gray-100 dark:bg-gray-900">
      <h1 className="text-3xl font-bold mb-6 text-gray-800 dark:text-gray-200">Custom Fields</h1>

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

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="md:col-span-1">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
            <h2 className="text-xl font-semibold mb-4 text-gray-700 dark:text-gray-300">General Settings</h2>
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

        <div className="md:col-span-2">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4 h-full">
            <fieldset disabled={!settings.auto_generate_custom_field} className="disabled:opacity-50">
              <h2 className="text-xl font-semibold mb-4 text-gray-700 dark:text-gray-300">Fields to process:</h2>
              <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
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
            </fieldset>
          </div>
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
    </div>
  );
};

export default CustomFieldsEditor;

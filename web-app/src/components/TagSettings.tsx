import { useEffect, useState } from 'react';
import axios from 'axios';

interface SettingsData {
  custom_fields_enable: boolean;
  custom_fields_selected_ids: number[];
  custom_fields_write_mode: 'append' | 'replace';
  tags_auto_create: boolean;
}

export default function TagSettings() {
  const [settings, setSettings] = useState<SettingsData>({
    custom_fields_enable: false,
    custom_fields_selected_ids: [],
    custom_fields_write_mode: 'append',
    tags_auto_create: false,
  });
  const [initialSettings, setInitialSettings] = useState<SettingsData>(settings);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  // Fetch settings on mount
  useEffect(() => {
    const fetchSettings = async () => {
      try {
        const response = await axios.get('./api/settings');
        const settingsData = response.data.settings as SettingsData;
        setSettings(settingsData);
        setInitialSettings(settingsData);
        setLoading(false);
      } catch (err) {
        console.error('Error fetching settings:', err);
        setError('Failed to load settings');
        setLoading(false);
      }
    };
    fetchSettings();
  }, []);

  // Check if settings have changed
  const hasChanges = JSON.stringify(settings) !== JSON.stringify(initialSettings);

  // Save settings
  const handleSave = async () => {
    setSaving(true);
    setMessage('');
    setError('');

    try {
      // Only send the field this component manages (partial update)
      const response = await axios.post('./api/settings', {
        tags_auto_create: settings.tags_auto_create
      });
      setMessage('Settings saved successfully');
      // Update both settings and initialSettings with the server response
      const settingsData = response.data.settings as SettingsData;
      setSettings(settingsData);
      setInitialSettings(settingsData);

      // Clear message after 3 seconds
      setTimeout(() => setMessage(''), 3000);
    } catch (err) {
      console.error('Error saving settings:', err);
      setError('Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  // Handle checkbox change
  const handleToggle = () => {
    setSettings((prev) => ({
      ...prev,
      tags_auto_create: !prev.tags_auto_create,
    }));
  };

  if (loading) {
    return <div className="text-gray-400">Loading settings...</div>;
  }

  return (
    <div className="p-6 bg-gray-100 dark:bg-gray-900">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-3xl font-bold text-gray-800 dark:text-gray-200">Tag Settings</h1>
      </div>

      <div className="bg-gray-800 p-4 rounded-lg space-y-4">
        {/* Tag Auto-Creation Toggle */}
        <div className="flex items-start space-x-3">
          <input
            type="checkbox"
            id="tagsAutoCreate"
            checked={settings.tags_auto_create}
            onChange={handleToggle}
            className="w-4 h-4 mt-1 text-blue-600 bg-gray-700 border-gray-600
                       rounded focus:ring-blue-500 focus:ring-2"
          />
          <div className="flex-1">
            <label
              htmlFor="tagsAutoCreate"
              className="block text-sm font-medium text-gray-200 cursor-pointer"
            >
              Automatically create new tags from AI suggestions
            </label>
            <p className="text-sm text-gray-400 mt-1">
              When enabled, tags suggested by the AI that don't exist in Paperless-ngx
              will be created automatically. When disabled, only existing tags will be used.
            </p>
            <div className="mt-2 p-2 bg-yellow-900/20 border border-yellow-700/50 rounded">
              <p className="text-xs text-yellow-400">
                ⚠️ This will modify your Paperless-ngx tag list.
                Review auto-created tags in Paperless-ngx settings.
              </p>
            </div>
          </div>
        </div>

        {/* Save Button */}
        <div className="flex items-center justify-between pt-4 border-t border-gray-700">
          <div className="flex-1">
            {message && (
              <p className="text-sm text-green-400">{message}</p>
            )}
            {error && (
              <p className="text-sm text-red-400">{error}</p>
            )}
          </div>
          <button
            onClick={handleSave}
            disabled={!hasChanges || saving}
            className={`px-4 py-2 rounded-lg font-medium transition-colors ${
              hasChanges && !saving
                ? 'bg-blue-600 hover:bg-blue-700 text-white'
                : 'bg-gray-700 text-gray-500 cursor-not-allowed'
            }`}
          >
            {saving ? 'Saving...' : 'Save Changes'}
          </button>
        </div>
      </div>
    </div>
  );
}

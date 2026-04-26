import axios from "axios";
import React, { useEffect, useState } from "react";

const taskLabels: Record<string, string> = {
  title: "Title model",
  tags: "Tags model",
  correspondent: "Correspondent model",
  document_type: "Document type model",
  created_date: "Created date model",
  custom_fields: "Custom fields model",
};

const providerLabels: Record<string, string> = {
  openai: "OpenAI",
  openrouter: "OpenRouter",
  ollama: "Ollama",
  googleai: "Google AI",
};

const providerDefaults: Record<string, { baseUrl: string; model: string }> = {
  openai: { baseUrl: "", model: "gpt-4o-mini" },
  openrouter: { baseUrl: "https://openrouter.ai/api/v1", model: "openai/gpt-4o-mini" },
  ollama: { baseUrl: "http://127.0.0.1:11434", model: "qwen3:8b" },
  googleai: { baseUrl: "", model: "gemini-2.0-flash" },
};

interface AIProviderSettings {
  provider: string;
  enabled: boolean;
  base_url: string;
  default_model: string;
  api_key_configured: boolean;
  task_models: Record<string, string>;
  source?: string;
}

const emptySettings: AIProviderSettings = {
  provider: "openai",
  enabled: false,
  base_url: "",
  default_model: "",
  api_key_configured: false,
  task_models: {},
};

const AIProvidersEditor: React.FC = () => {
  const [settings, setSettings] = useState<AIProviderSettings>(emptySettings);
  const [apiKey, setApiKey] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const load = async () => {
      try {
        const response = await axios.get<AIProviderSettings>("./api/ai/providers/settings");
        setSettings({
          ...emptySettings,
          ...response.data,
          task_models: response.data.task_models || {},
        });
      } catch (err) {
        console.error(err);
        setError("Failed to load AI provider settings.");
      } finally {
        setLoading(false);
      }
    };
    void load();
  }, []);

  const updateProvider = (provider: string) => {
    const defaults = providerDefaults[provider];
    setSettings((current) => ({
      ...current,
      provider,
      base_url: current.base_url || defaults.baseUrl,
      default_model: current.default_model || defaults.model,
    }));
  };

  const updateTaskModel = (task: string, model: string) => {
    setSettings((current) => ({
      ...current,
      task_models: {
        ...current.task_models,
        [task]: model,
      },
    }));
  };

  const payload = () => ({
    provider: settings.provider,
    enabled: settings.enabled,
    base_url: settings.base_url,
    default_model: settings.default_model,
    api_key: apiKey,
    task_models: settings.task_models,
  });

  const save = async () => {
    setSaving(true);
    setError(null);
    setMessage(null);
    try {
      const response = await axios.post<AIProviderSettings>("./api/ai/providers/settings", payload());
      setSettings({ ...response.data, task_models: response.data.task_models || {} });
      setApiKey("");
      setMessage("AI provider settings saved.");
    } catch (err) {
      console.error(err);
      setError("Failed to save AI provider settings.");
    } finally {
      setSaving(false);
    }
  };

  const testConnection = async () => {
    setTesting(true);
    setError(null);
    setMessage(null);
    try {
      const response = await axios.post<{ message: string }>("./api/ai/providers/test", payload());
      setMessage(response.data.message || "AI provider connection succeeded.");
    } catch (err) {
      console.error(err);
      if (axios.isAxiosError(err) && err.response?.data && typeof err.response.data.error === "string") {
        setError(err.response.data.error);
      } else {
        setError("AI provider connection failed.");
      }
    } finally {
      setTesting(false);
    }
  };

  if (loading) {
    return <p className="text-sm text-gray-500 dark:text-gray-400">Loading AI provider settings...</p>;
  }

  const requiresApiKey = settings.provider !== "ollama";
  const defaults = providerDefaults[settings.provider] || providerDefaults.openai;

  return (
    <section className="rounded-3xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="mb-5">
        <p className="text-sm font-semibold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">
          AI Providers
        </p>
        <h2 className="mt-2 text-2xl font-bold text-gray-900 dark:text-gray-100">
          UI-managed provider settings
        </h2>
        <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
          Configure the provider used for metadata suggestions. API keys are stored encrypted and never shown after saving.
        </p>
      </div>

      {error && <div className="mb-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">{error}</div>}
      {message && <div className="mb-4 rounded border border-green-200 bg-green-50 p-3 text-sm text-green-700 dark:border-green-900 dark:bg-green-950 dark:text-green-300">{message}</div>}

      <div className="grid gap-4 md:grid-cols-2">
        <label className="block">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Provider</span>
          <select
            value={settings.provider}
            onChange={(e) => updateProvider(e.target.value)}
            className="mt-1 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
          >
            {Object.entries(providerLabels).map(([value, label]) => (
              <option key={value} value={value}>{label}</option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-3 pt-6">
          <input
            type="checkbox"
            checked={settings.enabled}
            onChange={(e) => setSettings((current) => ({ ...current, enabled: e.target.checked }))}
            className="h-4 w-4"
          />
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Use UI settings instead of environment defaults
          </span>
        </label>

        {settings.provider !== "googleai" && (
          <label className="block">
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Base URL</span>
            <input
              type="text"
              value={settings.base_url}
              placeholder={defaults.baseUrl || "Provider default"}
              onChange={(e) => setSettings((current) => ({ ...current, base_url: e.target.value }))}
              className="mt-1 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
            />
          </label>
        )}

        <label className="block">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Default model</span>
          <input
            type="text"
            value={settings.default_model}
            placeholder={defaults.model}
            onChange={(e) => setSettings((current) => ({ ...current, default_model: e.target.value }))}
            className="mt-1 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
          />
        </label>

        {requiresApiKey && (
          <label className="block md:col-span-2">
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">API key</span>
            <input
              type="password"
              value={apiKey}
              placeholder={settings.api_key_configured ? "Configured - leave blank to keep existing key" : "Enter API key"}
              onChange={(e) => setApiKey(e.target.value)}
              className="mt-1 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
            />
          </label>
        )}
      </div>

      <button
        type="button"
        onClick={() => setShowAdvanced((value) => !value)}
        className="mt-5 text-sm font-medium text-indigo-600 hover:text-indigo-700 dark:text-indigo-400"
      >
        {showAdvanced ? "Hide" : "Show"} per-task model overrides
      </button>

      {showAdvanced && (
        <div className="mt-4 grid gap-4 md:grid-cols-2">
          {Object.entries(taskLabels).map(([task, label]) => (
            <label key={task} className="block">
              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">{label}</span>
              <input
                type="text"
                value={settings.task_models[task] || ""}
                placeholder="Use default model"
                onChange={(e) => updateTaskModel(task, e.target.value)}
                className="mt-1 w-full rounded border border-gray-300 px-3 py-2 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-100"
              />
            </label>
          ))}
        </div>
      )}

      <div className="mt-6 flex flex-wrap gap-3">
        <button
          type="button"
          onClick={() => { void testConnection(); }}
          disabled={testing}
          className="rounded-md bg-gray-200 px-4 py-2 text-sm font-medium text-gray-800 hover:bg-gray-300 disabled:opacity-60 dark:bg-gray-700 dark:text-gray-100 dark:hover:bg-gray-600"
        >
          {testing ? "Testing..." : "Test connection"}
        </button>
        <button
          type="button"
          onClick={() => { void save(); }}
          disabled={saving}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
        >
          {saving ? "Saving..." : "Save AI provider"}
        </button>
      </div>
    </section>
  );
};

export default AIProvidersEditor;

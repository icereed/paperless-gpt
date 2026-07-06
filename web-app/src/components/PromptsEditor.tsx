import React, { useState, useEffect } from 'react';

const PromptsEditor: React.FC = () => {
  const [prompts, setPrompts] = useState<Record<string, string>>({});
  const [selectedPrompt, setSelectedPrompt] = useState<string>('');
  const [content, setContent] = useState<string>('');
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [isSaving, setIsSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    fetch('./api/prompts', { signal: controller.signal })
      .then((res) => {
        if (!res.ok) {
          throw new Error('Network response was not ok');
        }
        return res.json();
      })
      .then((data) => {
        setPrompts(data);
        const first = Object.keys(data).sort()[0];
        if (first && !selectedPrompt) setSelectedPrompt(first);
        setIsLoading(false);
      })
      .catch((err) => {
        if (err.name !== 'AbortError') setError(err.message);
        setIsLoading(false);
      });
    return () => controller.abort();
  }, [selectedPrompt]);

  useEffect(() => {
    if (selectedPrompt && prompts[selectedPrompt]) {
      setContent(prompts[selectedPrompt]);
    } else {
      setContent('');
    }
  }, [selectedPrompt, prompts]);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const isModS = (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's';
      if (isModS) {
        e.preventDefault();
        if (!isSaving && selectedPrompt) handleSave();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isSaving, selectedPrompt, content]);

  const handleSave = () => {
    if (!selectedPrompt) return;

    setIsSaving(true);
    fetch('./api/prompts', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        filename: selectedPrompt,
        content: content,
      }),
    })
      .then((res) => {
        if (!res.ok) {
          const ct = res.headers.get('content-type') || '';
          if (ct.includes('application/json')) {
            return res.json().then((err) => { throw new Error(err.error || 'Failed to save prompt'); });
          }
          return res.text().then((txt) => { throw new Error(txt || 'Failed to save prompt'); });
        }
        return res.json();
      })
      .then(() => {
        setPrompts((prev) => ({ ...prev, [selectedPrompt]: content }));
        setSuccessMessage('Prompt saved successfully!');
        setTimeout(() => setSuccessMessage(null), 3000);
      })
      .catch((err) => {
        setError(err.message);
        setTimeout(() => setError(null), 5000);
      })
      .finally(() => setIsSaving(false));
  };

  if (isLoading) {
    return <div className="p-4">Loading prompts...</div>;
  }

  if (error && !successMessage) {
    return <div className="p-4 text-red-500">Error: {error}</div>;
  }

  return (
    <div className="p-6 bg-gray-100 dark:bg-gray-900">
      <h1 className="text-3xl font-bold mb-6 text-gray-800 dark:text-gray-200">Edit Prompts</h1>

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
            <h2 className="text-xl font-semibold mb-4 text-gray-700 dark:text-gray-300">Available Prompts</h2>
            <ul>
              {Object.keys(prompts).sort().map((filename) => (
                <li key={filename}
                  className={`p-2 rounded cursor-pointer transition-colors duration-200 ${selectedPrompt === filename ? 'bg-blue-500 text-white' : 'hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-800 dark:text-gray-300'}`}
                  onClick={() => setSelectedPrompt(filename)}
                >
                  {filename.replace(/_/g, ' ').replace('.tmpl', '')}
                </li>
              ))}
            </ul>
          </div>
        </div>

        <div className="md:col-span-2">
          {selectedPrompt ? (
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
              <h2 className="text-xl font-semibold mb-4 text-gray-700 dark:text-gray-300">
                Editing: <span className="font-mono text-blue-600 dark:text-blue-400">{selectedPrompt}</span>
              </h2>
              <textarea
                className="w-full h-96 p-3 border border-gray-300 dark:border-gray-600 rounded-md font-mono text-sm bg-gray-50 dark:bg-gray-700 text-gray-900 dark:text-gray-200 focus:ring-2 focus:ring-blue-500 focus:border-transparent transition"
                value={content}
                onChange={(e) => setContent(e.target.value)}
              />
              <div className="flex justify-end mt-6">
                <button
                  onClick={handleSave}
                  disabled={isSaving}
                  aria-busy={isSaving}
                  className={`px-6 py-2 rounded-md font-semibold focus:outline-none focus:ring-2 focus:ring-offset-2 transition-transform transform ${
                    isSaving
                      ? 'bg-blue-400 text-white cursor-not-allowed'
                      : 'bg-blue-600 text-white hover:bg-blue-700 hover:scale-105 focus:ring-blue-500'
                  }`}
                >
                  {isSaving ? 'Saving…' : 'Save Changes'}
                </button>
              </div>
            </div>
          ) : (
            <div className="flex items-center justify-center h-full bg-white dark:bg-gray-800 rounded-lg shadow p-4 text-gray-500 dark:text-gray-400">
              <p>Select a prompt from the list to start editing.</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default PromptsEditor;

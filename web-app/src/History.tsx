import React, { useEffect, useState } from 'react';
import UndoCard from './components/UndoCard';

interface ModificationHistory {
  ID: number;
  DocumentID: number;
  DateChanged: string;
  ModField: string;
  PreviousValue: string;
  NewValue: string;
  Undone: boolean;
  UndoneDate: string | null;
}

const History: React.FC = () => {
  const [modifications, setModifications] = useState<ModificationHistory[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [paperlessUrl, setPaperlessUrl] = useState<string>('');

  // Get Paperless URL
  useEffect(() => {
    const fetchUrl = async () => {
      try {
        const response = await fetch('/api/paperless-url');
        if (!response.ok) {
          throw new Error('Failed to fetch public URL');
        }
        const { url } = await response.json();
        setPaperlessUrl(url);
      } catch (err) {
        console.error('Error fetching Paperless URL:', err);
      }
    };
    
    fetchUrl();
  }, []);

  // Get all modifications
  useEffect(() => {
    fetchModifications();
  }, []);

  const fetchModifications = async () => {
    try {
      const response = await fetch('/api/modifications');
      if (!response.ok) {
        throw new Error('Failed to fetch modifications');
      }
      const data = await response.json();
      setModifications(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error occurred');
    } finally {
      setLoading(false);
    }
  };

  const handleUndo = async (id: number) => {
    try {
      const response = await fetch(`/api/undo-modification/${id}`, {
        method: 'POST',
      });
      
      if (!response.ok) {
        throw new Error('Failed to undo modification');
      }
  
      // Use ISO 8601 format for consistency
      const now = new Date().toISOString();
      
      setModifications(mods => mods.map(mod => 
        mod.ID === id
          ? { ...mod, Undone: true, UndoneDate: now }
          : mod
      ));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to undo modification');
    }
  };

  if (loading) {
    return (
      <div className="flex justify-center items-center min-h-screen">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-red-500 dark:text-red-400 p-4 text-center">
        Error: {error}
      </div>
    );
  }

  return (
    <div className="container mx-auto px-4 py-8">
      <h1 className="text-2xl font-bold text-gray-800 dark:text-gray-200">
        Modification History
      </h1>
      <div className="mb-6 text-sm text-gray-500 dark:text-gray-400">
        Note: when undoing tag changes, this will not re-add 'paperless-gpt-auto'
      </div>
      {modifications.length === 0 ? (
        <p className="text-gray-500 dark:text-gray-400 text-center">
          No modifications found
        </p>
      ) : (
        <div className="grid gap-4 md:grid-cols-1 lg:grid-cols-1">
          {modifications.map((modification) => (
            <UndoCard
              key={modification.ID}
              {...modification}
              onUndo={handleUndo}
              paperlessUrl={paperlessUrl}
            />
          ))}
        </div>
      )}
    </div>
  );
};

export default History;
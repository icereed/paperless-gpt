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

interface PaginatedResponse {
  items: ModificationHistory[];
  totalItems: number;
  totalPages: number;
  currentPage: number;
  pageSize: number;
}

const History: React.FC = () => {
  const [modifications, setModifications] = useState<ModificationHistory[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [paperlessUrl, setPaperlessUrl] = useState<string>('');
  const [currentPage, setCurrentPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [totalItems, setTotalItems] = useState(0);
  const pageSize = 20;

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

  // Get modifications with pagination
  useEffect(() => {
    fetchModifications(currentPage);
  }, [currentPage]);

  const fetchModifications = async (page: number) => {
    setLoading(true);
    try {
      const response = await fetch(`/api/modifications?page=${page}&pageSize=${pageSize}`);
      if (!response.ok) {
        throw new Error('Failed to fetch modifications');
      }
      const data: PaginatedResponse = await response.json();
      setModifications(data.items);
      setTotalPages(data.totalPages);
      setTotalItems(data.totalItems);
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
    <div className="modification-history container mx-auto px-4 py-8">
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
        <>
          <div className="grid gap-4 md:grid-cols-1 lg:grid-cols-1 mb-6">
            {modifications.map((modification) => (
              <UndoCard
                key={modification.ID}
                {...modification}
                onUndo={handleUndo}
                paperlessUrl={paperlessUrl}
              />
            ))}
          </div>
          <div className="flex items-center justify-between border-t border-gray-200 dark:border-gray-700 pt-4">
            <div className="flex items-center text-sm text-gray-500 dark:text-gray-400">
              <span>
                Showing {((currentPage - 1) * pageSize) + 1} to {Math.min(currentPage * pageSize, totalItems)} of {totalItems} results
              </span>
            </div>
            <div className="flex items-center space-x-2">
              <button
                onClick={() => setCurrentPage(page => Math.max(1, page - 1))}
                disabled={currentPage === 1}
                className={`px-3 py-1 rounded-md ${
                  currentPage === 1
                    ? 'bg-gray-100 text-gray-400 cursor-not-allowed dark:bg-gray-800'
                    : 'bg-blue-500 text-white hover:bg-blue-600 dark:bg-blue-600 dark:hover:bg-blue-700'
                }`}
              >
                Previous
              </button>
              <span className="text-sm text-gray-600 dark:text-gray-300">
                Page {currentPage} of {totalPages}
              </span>
              <button
                onClick={() => setCurrentPage(page => Math.min(totalPages, page + 1))}
                disabled={currentPage === totalPages}
                className={`px-3 py-1 rounded-md ${
                  currentPage === totalPages
                    ? 'bg-gray-100 text-gray-400 cursor-not-allowed dark:bg-gray-800'
                    : 'bg-blue-500 text-white hover:bg-blue-600 dark:bg-blue-600 dark:hover:bg-blue-700'
                }`}
              >
                Next
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
};

export default History;

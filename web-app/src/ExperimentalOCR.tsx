import axios from 'axios';
import React, { useCallback, useEffect, useState } from 'react';
import { FaSpinner } from 'react-icons/fa';
import { Document, DocumentSuggestion } from './DocumentProcessor';

const ExperimentalOCR: React.FC = () => {
  const refreshInterval = 1000; // Refresh interval in milliseconds
  const [documentId, setDocumentId] = useState(0);
  const [jobId, setJobId] = useState('');
  const [ocrResult, setOcrResult] = useState('');
  const [status, setStatus] = useState('');
  const [error, setError] = useState<string | null>('');
  const [pagesDone, setPagesDone] = useState(0); // New state for pages done
  const [saving, setSaving] = useState(false); // New state for saving
  const [documentDetails, setDocumentDetails] = useState<Document | null>(null); // New state for document details

  const submitOCRJob = async () => {
    setStatus('');
    setError('');
    setJobId('');
    setOcrResult('');
    setPagesDone(0); // Reset pages done

    try {
      setStatus('Submitting OCR job...');
      const response = await axios.post(`/api/documents/${documentId}/ocr`);
      setJobId(response.data.job_id);
      setStatus('Job submitted. Processing...');
    } catch (err) {
      console.error(err);
      setError('Failed to submit OCR job.');
    }
  };

  const checkJobStatus = async () => {
    if (!jobId) return;

    try {
      const response = await axios.get(`/api/jobs/ocr/${jobId}`);
      const jobStatus = response.data.status;
      setPagesDone(response.data.pages_done); // Update pages done
      if (jobStatus === 'completed') {
        setOcrResult(response.data.result);
        setStatus('OCR completed successfully.');
      } else if (jobStatus === 'failed') {
        setError(response.data.error);
        setStatus('OCR failed.');
      } else {
        setStatus(`Job status: ${jobStatus}. This may take a few minutes.`);
        // Automatically check again after a delay
        setTimeout(checkJobStatus, refreshInterval);
      }
    } catch (err) {
      console.error(err);
      setError('Failed to check job status.');
    } 
  };

  const handleSaveContent = async () => {
    setSaving(true);
    setError(null);
    try {
      if (!documentDetails) {
        setError('Document details not fetched.');
        throw new Error('Document details not fetched.');
      }
      const requestPayload: DocumentSuggestion = {
        id: documentId,
        original_document: documentDetails, // Use fetched document details
        suggested_content: ocrResult,
      };

      await axios.post("/api/save-content", requestPayload);
      setStatus('Content saved successfully.');
    } catch (err) {
      console.error("Error saving content:", err);
      setError("Failed to save content.");
    } finally {
      setSaving(false);
    }
  };

  const fetchDocumentDetails = useCallback(async () => {
    if (!documentId) return;

    try {
      const response = await axios.get<Document>(`/api/documents/${documentId}`);
      setDocumentDetails(response.data);
    } catch (err) {
      console.error("Error fetching document details:", err);
      setError("Failed to fetch document details.");
    }
  }, [documentId]);

  // Fetch document details when documentId changes
  useEffect(() => {
    fetchDocumentDetails();
  }, [documentId, fetchDocumentDetails]);

  // Start checking job status when jobId is set
  useEffect(() => {
    if (jobId) {
      checkJobStatus();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobId]);

  return (
    <div className="max-w-3xl mx-auto p-6 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200">
      <h1 className="text-4xl font-bold mb-6 text-center">OCR via LLMs (Experimental)</h1>
      <p className="mb-6 text-center text-yellow-600">
        This is an experimental feature. Results may vary, and processing may take some time.
      </p>
      <div className="bg-gray-100 dark:bg-gray-800 p-6 rounded-lg shadow-md">
        <div className="mb-4">
          <label htmlFor="documentId" className="block mb-2 font-semibold">
            Document ID:
          </label>
          <input
            type="number"
            id="documentId"
            value={documentId}
            onChange={(e) => setDocumentId(Number(e.target.value))}
            className="border border-gray-300 dark:border-gray-700 rounded w-full p-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
            placeholder="Enter the document ID"
          />
        </div>
        <button
          onClick={submitOCRJob}
          className="w-full bg-blue-600 hover:bg-blue-700 text-white font-semibold py-2 px-4 rounded transition duration-200"
          disabled={!documentId}
        >
          {status.startsWith('Submitting') ? (
            <span className="flex items-center justify-center">
              <FaSpinner className="animate-spin mr-2" />
              Submitting...
            </span>
          ) : (
            'Submit OCR Job'
          )}
        </button>
        {status && (
          <div className="mt-4 text-center text-gray-700 dark:text-gray-300">
            {status.includes('in_progress') && (
              <span className="flex items-center justify-center">
                <FaSpinner className="animate-spin mr-2" />
                {status}
              </span>
            )}
            {!status.includes('in_progress') && status}
            {pagesDone > 0 && (
              <div className="mt-2">
                Pages processed: {pagesDone}
              </div>
            )}
          </div>
        )}
        {error && (
          <div className="mt-4 p-4 bg-red-100 dark:bg-red-800 text-red-700 dark:text-red-200 rounded">
            {error}
          </div>
        )}
        {ocrResult && (
          <div className="mt-6">
            <h2 className="text-2xl font-bold mb-4">OCR Result:</h2>
            <div className="bg-gray-50 dark:bg-gray-900 p-4 rounded border border-gray-200 dark:border-gray-700 overflow-auto max-h-96">
              <pre className="whitespace-pre-wrap">{ocrResult}</pre>
            </div>
            <button
              onClick={handleSaveContent}
              className="w-full bg-green-600 hover:bg-green-700 text-white font-semibold py-2 px-4 rounded transition duration-200 mt-4"
              disabled={saving}
            >
              {saving ? (
                <span className="flex items-center justify-center">
                  <FaSpinner className="animate-spin mr-2" />
                  Saving...
                </span>
              ) : (
                'Save Content'
              )}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export default ExperimentalOCR;
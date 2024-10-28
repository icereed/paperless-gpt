// ExperimentalOCR.tsx
import axios from 'axios';
import React, { useState } from 'react';
import { FaSpinner } from 'react-icons/fa';


const ExperimentalOCR: React.FC = () => {
  const [documentId, setDocumentId] = useState('');
  const [jobId, setJobId] = useState('');
  const [ocrResult, setOcrResult] = useState('');
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');
  const [isCheckingStatus, setIsCheckingStatus] = useState(false);

  const submitOCRJob = async () => {
    setStatus('');
    setError('');
    setJobId('');
    setOcrResult('');

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
    setIsCheckingStatus(true);

    try {
      const response = await axios.get(`/api/jobs/ocr/${jobId}`);
      const jobStatus = response.data.status;
      if (jobStatus === 'completed') {
        setOcrResult(response.data.result);
        setStatus('OCR completed successfully.');
      } else if (jobStatus === 'failed') {
        setError(response.data.error);
        setStatus('OCR failed.');
      } else {
        setStatus(`Job status: ${jobStatus}. This may take a few minutes.`);
        // Automatically check again after a delay
        setTimeout(checkJobStatus, 5000);
      }
    } catch (err) {
      console.error(err);
      setError('Failed to check job status.');
    } finally {
      setIsCheckingStatus(false);
    }
  };

  // Start checking job status when jobId is set
  React.useEffect(() => {
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
            type="text"
            id="documentId"
            value={documentId}
            onChange={(e) => setDocumentId(e.target.value)}
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
          </div>
        )}
      </div>
    </div>
  );
};

export default ExperimentalOCR;

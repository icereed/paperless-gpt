import React, { useState, useEffect } from 'react';
import axios from 'axios';
import { Document } from './DocumentProcessor';
import DocumentsToProcess from './components/DocumentsToProcess';

const AdhocAnalysis: React.FC = () => {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedDocuments, setSelectedDocuments] = useState<number[]>([]);
  const [prompt, setPrompt] = useState('');
  const [originalPrompt, setOriginalPrompt] = useState('');
  const [analysisResult, setAnalysisResult] = useState('');
  const [error, setError] = useState('');
  const [processing, setProcessing] = useState(false);
  const [loading, setLoading] = useState(true);

  const fetchDocuments = async () => {
    try {
      setLoading(true);
      const res = await axios.get<Document[]>('./api/documents');
      setDocuments(res.data || []);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  const fetchPromptTemplate = async () => {
    try {
      const res = await axios.get<{[key: string]: string}>('./api/prompts');
      const defaultPrompt = res.data['adhoc-analysis_prompt.tmpl'] || '';
      setPrompt(defaultPrompt);
      setOriginalPrompt(defaultPrompt);
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    fetchDocuments();
    fetchPromptTemplate();
  }, []);

  const handleSelectDocument = (docId: number) => {
    setSelectedDocuments((prev) =>
      prev.includes(docId)
        ? prev.filter((id) => id !== docId)
        : [...prev, docId]
    );
  };

  const handleSelectAll = () => {
    setSelectedDocuments(documents.map((doc) => doc.id));
  };

  const handleSelectNone = () => {
    setSelectedDocuments([]);
  };

  const handleStartAnalysis = async () => {
    try {
      setProcessing(true);
      setError('');
      setAnalysisResult('');
      const res = await axios.post<{result: string}>('./api/analyze-documents', {
        document_ids: selectedDocuments,
        prompt,
      });
      setAnalysisResult(res.data.result);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'An unknown error occurred.');
    } finally {
      setProcessing(false);
    }
  };

  return (
    <div className="p-6">
      <h1 className="text-3xl font-bold mb-6">Ad-hoc Analysis</h1>

      <div className="mb-6">
        <div className="flex justify-between items-center mb-4">
            <h2 className="text-2xl font-semibold text-gray-700 dark:text-gray-200">Documents to Analyze</h2>
            <div className="flex space-x-2">
                <button onClick={handleSelectAll} className="bg-gray-200 dark:bg-gray-600 px-4 py-2 rounded">Select All</button>
                <button onClick={handleSelectNone} className="bg-gray-200 dark:bg-gray-600 px-4 py-2 rounded">Select None</button>
            </div>
        </div>
        {loading ? (
          <p>Loading documents...</p>
        ) : (
          <DocumentsToProcess
            documents={documents}
            selectedDocuments={selectedDocuments}
            onSelectDocument={handleSelectDocument}
            gridCols="3"
          />
        )}
      </div>

      <div className="mb-6">
        <h2 className="text-xl font-semibold mb-2">Analysis Prompt</h2>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          className="w-full h-48 p-2 border rounded dark:bg-gray-700 dark:border-gray-600"
        />
      </div>

      <div className="mb-6 flex justify-end items-center space-x-4">
        <button
          onClick={() => setPrompt(originalPrompt)}
          className="bg-gray-300 dark:bg-gray-600 px-4 py-2 rounded hover:bg-gray-400 dark:hover:bg-gray-500"
        >
          Reset to Default
        </button>
        <button
          onClick={handleStartAnalysis}
          disabled={processing || selectedDocuments.length === 0}
          className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 disabled:bg-gray-400"
        >
          {processing ? 'Analyzing...' : 'Start Analysis'}
        </button>
      </div>

      <div className="mb-6">
        <h2 className="text-xl font-semibold mb-2">Analysis Result</h2>
        <div className={`p-4 border rounded ${!analysisResult && !error ? 'bg-gray-200 dark:bg-gray-800 text-gray-500' : 'bg-gray-50 dark:bg-gray-700 dark:border-gray-600'}`}>
          {error ? (
            <pre className="whitespace-pre-wrap text-red-500">{error}</pre>
          ) : analysisResult ? (
            <pre className="whitespace-pre-wrap">{analysisResult}</pre>
          ) : (
            <p>Start analysis to show results</p>
          )}
        </div>
      </div>
    </div>
  );
};

export default AdhocAnalysis;

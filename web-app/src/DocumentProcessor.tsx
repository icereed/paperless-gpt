import axios from "axios";
import React, { useCallback, useEffect, useRef, useState } from "react";
import "react-tag-autocomplete/example/src/styles.css"; // Ensure styles are loaded
import DocumentsToProcess from "./components/DocumentsToProcess";
import NoDocuments from "./components/NoDocuments";
import ArrowPathIcon from "@heroicons/react/24/outline/ArrowPathIcon";
import SuccessModal from "./components/SuccessModal";
import SuggestionsReview from "./components/SuggestionsReview";

export interface Document {
  id: number;
  title: string;
  content: string;
  tags: string[];
  correspondent: string;
}

export interface GenerateSuggestionsRequest {
  documents: Document[];
  generate_titles?: boolean;
  generate_tags?: boolean;
  generate_correspondents?: boolean;
  generate_document_types?: boolean;
  generate_created_date?: boolean;
  generate_custom_fields?: boolean;
  selected_custom_field_ids?: number[];
  custom_field_write_mode?: string;
}

export interface CustomFieldSuggestion {
  id: number;
  value: unknown;
  name: string;
  isSelected: boolean;
}

export interface DocumentSuggestion {
  id: number;
  original_document: Document;
  suggested_title?: string;
  suggested_tags?: string[];
  suggested_content?: string;
  suggested_correspondent?: string;
  suggested_document_type?: string;
  suggested_created_date?: string;
  suggested_custom_fields?: CustomFieldSuggestion[];
}

export interface TagOption {
  id: string;
  name: string;
}

interface SuggestionJobResponse {
  job_id: string;
  status: "pending" | "in_progress" | "completed" | "failed" | "cancelled";
  documents_done: number;
  total_documents: number;
  current_document_id?: number;
  result?: DocumentSuggestion[];
  error?: string;
}

interface CustomField {
  id: number;
  name: string;
  data_type: string;
}

const activeSuggestionJobStorageKey = "paperless-gpt-active-suggestion-job-id";
const suggestionJobPollIntervalMs = 1500;

const DocumentProcessor: React.FC = () => {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedDocuments, setSelectedDocuments] = useState<number[]>([]);
  const [suggestions, setSuggestions] = useState<DocumentSuggestion[]>([]);
  const [availableTags, setAvailableTags] = useState<TagOption[]>([]);
  const [allCustomFields, setAllCustomFields] = useState<CustomField[]>([]);
  const [loading, setLoading] = useState(true);
  const [processing, setProcessing] = useState(false);
  const [suggestionJobId, setSuggestionJobId] = useState<string>(() => localStorage.getItem(activeSuggestionJobStorageKey) || "");
  const [suggestionJobStatus, setSuggestionJobStatus] = useState<SuggestionJobResponse["status"] | "idle">("idle");
  const [documentsDone, setDocumentsDone] = useState(0);
  const [totalDocuments, setTotalDocuments] = useState(0);
  const [currentDocumentId, setCurrentDocumentId] = useState<number | null>(null);
  const [updating, setUpdating] = useState(false);
  const [isSuccessModalOpen, setIsSuccessModalOpen] = useState(false);
  const [filterTag, setFilterTag] = useState<string | null>(null);
  const [generateTitles, setGenerateTitles] = useState(true);
  const [generateTags, setGenerateTags] = useState(true);
  const [generateCorrespondents, setGenerateCorrespondents] = useState(true);
  const [generateDocumentTypes, setGenerateDocumentTypes] = useState(true);
  const [generateCreatedDate, setGenerateCreatedDate] = useState(true);
  const [generateCustomFields, setGenerateCustomFields] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const pollTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const processSuggestionResults = useCallback((data: DocumentSuggestion[]) => {
    const customFieldMap = new Map((allCustomFields || []).map(cf => [cf.id, cf.name]));
    const processedSuggestions = data.map(suggestion => ({
      ...suggestion,
      suggested_custom_fields: suggestion.suggested_custom_fields?.map(cf => ({
        ...cf,
        name: customFieldMap.get(cf.id) ?? cf.name ?? 'Unknown Field',
        isSelected: true,
      })),
    }));

    setSuggestions(processedSuggestions);
  }, [allCustomFields]);

  const clearActiveSuggestionJob = useCallback(() => {
    if (pollTimeoutRef.current) {
      clearTimeout(pollTimeoutRef.current);
      pollTimeoutRef.current = null;
    }
    localStorage.removeItem(activeSuggestionJobStorageKey);
    setSuggestionJobId("");
    setProcessing(false);
    setCurrentDocumentId(null);
  }, []);

  // Custom hook to fetch initial data
  const fetchInitialData = useCallback(async () => {
    try {
      const [filterTagRes, documentsRes, tagsRes, customFieldsRes] = await Promise.all([
        axios.get<{ tag: string }>("./api/filter-tag"),
        axios.get<Document[]>("./api/documents"),
        axios.get<Record<string, number>>("./api/tags"),
        axios.get<CustomField[]>('./api/custom_fields'),
      ]);

      setFilterTag(filterTagRes.data.tag);
      setAllCustomFields(customFieldsRes.data || []);
      setDocuments(documentsRes.data);
      setSelectedDocuments(documentsRes.data.map((doc) => doc.id));
      const tags = Object.keys(tagsRes.data).map((tag) => ({
        id: tag,
        name: tag,
      }));
      setAvailableTags(tags);
    } catch (err) {
      console.error("Error fetching initial data:", err);
      setError("Failed to fetch initial data.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchInitialData();
  }, [fetchInitialData]);

  const pollSuggestionJob = useCallback(async (jobId: string) => {
    if (!jobId) return;

    try {
      const { data } = await axios.get<SuggestionJobResponse>(`./api/jobs/suggestions/${jobId}`);
      setSuggestionJobStatus(data.status);
      setDocumentsDone(data.documents_done || 0);
      setTotalDocuments(data.total_documents || 0);
      setCurrentDocumentId(data.current_document_id || null);

      if (data.status === "completed") {
        processSuggestionResults(data.result || []);
        clearActiveSuggestionJob();
      } else if (data.status === "failed" || data.status === "cancelled") {
        setError(data.error || `Suggestion job ${data.status}.`);
        clearActiveSuggestionJob();
      } else {
        setProcessing(true);
        pollTimeoutRef.current = setTimeout(() => pollSuggestionJob(jobId), suggestionJobPollIntervalMs);
      }
    } catch (err) {
      console.error("Error checking suggestion job status:", err);
      if (axios.isAxiosError(err) && (err.response?.status === 404 || err.response?.status === 410)) {
        setError("Suggestion job is no longer available.");
        clearActiveSuggestionJob();
        return;
      }
      setError("Failed to check suggestion job status. Retrying...");
      setProcessing(true);
      pollTimeoutRef.current = setTimeout(() => pollSuggestionJob(jobId), suggestionJobPollIntervalMs);
    }
  }, [clearActiveSuggestionJob, processSuggestionResults]);

  useEffect(() => {
    if (loading) {
      return;
    }

    if (suggestionJobId) {
      setProcessing(true);
      pollSuggestionJob(suggestionJobId);
    }

    return () => {
      if (pollTimeoutRef.current) {
        clearTimeout(pollTimeoutRef.current);
        pollTimeoutRef.current = null;
      }
    };
  }, [loading, pollSuggestionJob, suggestionJobId]);

  const handleSelectDocument = (documentId: number) => {
    setSelectedDocuments((previous) =>
      previous.includes(documentId)
        ? previous.filter((id) => id !== documentId)
        : [...previous, documentId]
    );
  };

  const handleProcessDocuments = async () => {
    const documentsToProcess = documents.filter((doc) => selectedDocuments.includes(doc.id));
    if (documentsToProcess.length === 0) {
      setError("Select at least one document to process.");
      return;
    }

    setProcessing(true);
    setError(null);
    setDocumentsDone(0);
    setTotalDocuments(documentsToProcess.length);
    setCurrentDocumentId(null);
    try {
      const requestPayload: GenerateSuggestionsRequest = {
        documents: documentsToProcess,
        generate_titles: generateTitles,
        generate_tags: generateTags,
        generate_correspondents: generateCorrespondents,
        generate_document_types: generateDocumentTypes,
        generate_created_date: generateCreatedDate,
        generate_custom_fields: generateCustomFields,
      };

      const { data } = await axios.post<{ job_id: string }>(
        "./api/jobs/suggestions",
        requestPayload
      );

      localStorage.setItem(activeSuggestionJobStorageKey, data.job_id);
      setSuggestionJobId(data.job_id);
      setSuggestionJobStatus("pending");
    } catch (err) {
      console.error("Error generating suggestions:", err);
      setError("Failed to submit suggestion job.");
      setProcessing(false);
    }
  };

  const handleStopSuggestionJob = async () => {
    if (!suggestionJobId) return;

    try {
      await axios.post(`./api/jobs/suggestions/${suggestionJobId}/stop`);
      setSuggestionJobStatus("cancelled");
      setError("Suggestion job cancelled.");
      clearActiveSuggestionJob();
    } catch (err) {
      console.error("Error stopping suggestion job:", err);
      setError("Failed to stop suggestion job.");
    }
  };

  const handleUpdateDocuments = async () => {
    setUpdating(true);
    setError(null);
    try {
      // Filter out deselected custom fields before sending
      const payload = suggestions.map(suggestion => {
        const { suggested_custom_fields, ...rest } = suggestion;
        return {
          ...rest,
          suggested_custom_fields: suggested_custom_fields?.filter(cf => cf.isSelected),
        };
      });

      await axios.patch("./api/update-documents", payload);
      setIsSuccessModalOpen(true);
      setSuggestions([]);
    } catch (err) {
      console.error("Error updating documents:", err);
      setError("Failed to update documents.");
    } finally {
      setUpdating(false);
    }
  };

  const handleTagAddition = (docId: number, tag: TagOption) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId
          ? {
              ...doc,
              suggested_tags: [...(doc.suggested_tags || []), tag.name],
            }
          : doc
      )
    );
  };

  const handleCustomFieldSuggestionToggle = (docId: number, fieldId: number) => {
    setSuggestions(prevSuggestions =>
      prevSuggestions.map(doc =>
        doc.id === docId
          ? {
              ...doc,
              suggested_custom_fields: doc.suggested_custom_fields?.map(cf =>
                cf.id === fieldId ? { ...cf, isSelected: !cf.isSelected } : cf
              ),
            }
          : doc
      )
    );
  };

  const handleTagDeletion = (docId: number, index: number) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId
          ? {
              ...doc,
              suggested_tags: doc.suggested_tags?.filter((_, i) => i !== index),
            }
          : doc
      )
    );
  };


  const handleTitleChange = (docId: number, title: string) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId ? { ...doc, suggested_title: title } : doc
      )
    );
  };

  const handleCorrespondentChange = (docId: number, correspondent: string) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId ? { ...doc, suggested_correspondent: correspondent } : doc
      )
    );
  }

  const handleDocumentTypeChange = (docId: number, documentType: string) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId ? { ...doc, suggested_document_type: documentType } : doc
      )
    );
  }

  const handleCreatedDateChange = (docId: number, createdDate: string) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId ? { ...doc, suggested_created_date: createdDate } : doc
      )
    );
  }

  const resetSuggestions = () => {
    setSuggestions([]);
  };

  const reloadDocuments = async () => {
    setLoading(true);
    setError(null);
    try {
      const { data } = await axios.get<Document[]>("./api/documents");
      setDocuments(data);
      setSelectedDocuments(data.map((doc) => doc.id));
    } catch (err) {
      console.error("Error reloading documents:", err);
      setError("Failed to reload documents.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (documents.length === 0) {
      const interval = setInterval(async () => {
        setError(null);
        try {
          const { data } = await axios.get<Document[]>("./api/documents");
          setDocuments(data);
          setSelectedDocuments(data.map((doc) => doc.id));
        } catch (err) {
          console.error("Error reloading documents:", err);
          setError("Failed to reload documents.");
        }
      }, 500);
      return () => clearInterval(interval);
    }
  }, [documents]);

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen bg-white dark:bg-gray-900">
        <div className="text-xl font-semibold text-gray-800 dark:text-gray-200">
          Loading documents...
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto p-6 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200">
      <header className="text-center">
        <h1 className="text-4xl font-bold mb-8">Paperless GPT</h1>
      </header>

      {error && (
        <div className="mb-4 p-4 bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-200 rounded">
          {error}
        </div>
      )}

      {documents.length === 0 ? (
        <NoDocuments
          filterTag={filterTag}
          onReload={reloadDocuments}
          processing={processing}
        />
      ) : suggestions.length === 0 ? (
        <DocumentsToProcess
          documents={documents}
          selectedDocuments={selectedDocuments}
          onSelectDocument={handleSelectDocument}
        >
          <div className="flex justify-between items-center mb-6">
            <h2 className="text-2xl font-semibold text-gray-700 dark:text-gray-200">Documents to Process</h2>
            <div className="flex space-x-2">
              <button
                onClick={reloadDocuments}
                disabled={processing}
                className="bg-blue-600 text-white dark:bg-blue-800 dark:text-gray-200 px-4 py-2 rounded hover:bg-blue-700 dark:hover:bg-blue-900 focus:outline-none"
              >
                <ArrowPathIcon className="h-5 w-5" />
              </button>
              <button
                onClick={handleProcessDocuments}
                disabled={processing || selectedDocuments.length === 0 || !(generateTitles || generateTags || generateCorrespondents || generateDocumentTypes || generateCreatedDate || generateCustomFields)}
                className="bg-blue-600 text-white dark:bg-blue-800 dark:text-gray-200 px-4 py-2 rounded hover:bg-blue-700 dark:hover:bg-blue-900 focus:outline-none"
              >
                {processing ? "Processing..." : "Generate Suggestions"}
              </button>
              {processing && suggestionJobId && (
                <button
                  onClick={handleStopSuggestionJob}
                  className="bg-red-600 text-white dark:bg-red-800 dark:text-gray-200 px-4 py-2 rounded hover:bg-red-700 dark:hover:bg-red-900 focus:outline-none"
                >
                  Stop
                </button>
              )}
            </div>
          </div>

          {processing && (
            <div className="mb-4 p-3 bg-blue-50 dark:bg-blue-950 text-blue-800 dark:text-blue-200 rounded">
              Suggestion job {suggestionJobStatus}: {documentsDone} / {totalDocuments} documents processed
              {currentDocumentId ? `, current document ${currentDocumentId}` : ""}
            </div>
          )}

          <div className="flex space-x-4 mb-6">
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={generateTitles}
                onChange={(e) => setGenerateTitles(e.target.checked)}
                className="dark:bg-gray-700 dark:border-gray-600"
              />
              <span className="text-gray-700 dark:text-gray-200">Generate Titles</span>
            </label>
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={generateTags}
                onChange={(e) => setGenerateTags(e.target.checked)}
                className="dark:bg-gray-700 dark:border-gray-600"
              />
              <span className="text-gray-700 dark:text-gray-200">Generate Tags</span>
            </label>
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={generateCorrespondents}
                onChange={(e) => setGenerateCorrespondents(e.target.checked)}
                className="dark:bg-gray-700 dark:border-gray-600"
              />
              <span className="text-gray-700 dark:text-gray-200">Generate Correspondents</span>
            </label>
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={generateDocumentTypes}
                onChange={(e) => setGenerateDocumentTypes(e.target.checked)}
                className="dark:bg-gray-700 dark:border-gray-600"
              />
              <span className="text-gray-700 dark:text-gray-200">Generate Document Types</span>
            </label>
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={generateCreatedDate}
                onChange={(e) => setGenerateCreatedDate(e.target.checked)}
                className="dark:bg-gray-700 dark:border-gray-600"
              />
              <span className="text-gray-700 dark:text-gray-200">Generate Created Date</span>
            </label>
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={generateCustomFields}
                onChange={(e) => setGenerateCustomFields(e.target.checked)}
                className="dark:bg-gray-700 dark:border-gray-600"
              />
              <span className="text-gray-700 dark:text-gray-200">Generate Custom Fields</span>
            </label>
          </div>
        </DocumentsToProcess>
      ) : (
        <SuggestionsReview
          suggestions={suggestions}
          availableTags={availableTags}
          onTitleChange={handleTitleChange}
          onTagAddition={handleTagAddition}
          onTagDeletion={handleTagDeletion}
          onCorrespondentChange={handleCorrespondentChange}
          onDocumentTypeChange={handleDocumentTypeChange}
          onCreatedDateChange={handleCreatedDateChange}
          onCustomFieldSuggestionToggle={handleCustomFieldSuggestionToggle}
          onBack={resetSuggestions}
          onUpdate={handleUpdateDocuments}
          updating={updating}
        />
      )}

      <SuccessModal
        isOpen={isSuccessModalOpen}
        onClose={() => {
          setIsSuccessModalOpen(false);
          reloadDocuments();
        }}
      />
    </div>
  );
};

export default DocumentProcessor;

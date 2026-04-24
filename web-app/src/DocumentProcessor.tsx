import axios from "axios";
import React, { useCallback, useEffect, useState } from "react";
import "react-tag-autocomplete/example/src/styles.css"; // Ensure styles are loaded
import DocumentsToProcess from "./components/DocumentsToProcess";
import NoDocuments from "./components/NoDocuments";
import ArrowPathIcon from "@heroicons/react/24/outline/ArrowPathIcon";
import SuccessModal from "./components/SuccessModal";
import SuggestionsReview from "./components/SuggestionsReview";

export interface DocumentCustomField {
  field: number;
  value: unknown;
  name?: string;
}

export interface Document {
  id: number;
  title: string;
  content: string;
  tags: string[];
  correspondent: string;
  created_date?: string;
  original_file_name?: string;
  archived_file_name?: string;
  document_type_name?: string;
  custom_fields?: DocumentCustomField[];
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

export interface JobberMatchCandidate {
  id: string;
  job_number: string;
  client_name: string;
  job_name: string;
}

export interface DocumentIntegrationResult {
  document_id: number;
  paperless_updated: boolean;
  jobber_applied: boolean;
  jobber_error?: string;
  jobber_expense_created?: boolean;
  jobber_expense_id?: string;
  jobber_expense_error?: string;
  google_drive_uploaded: boolean;
  google_drive_error?: string;
  google_drive_file_id?: string;
  google_drive_url?: string;
}

export interface IntegrationStatus {
  provider: string;
  configured: boolean;
  connected: boolean;
  account_name?: string;
  account_id?: string;
  reason?: string;
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
  custom_fields_write_mode?: string;
  jobber_candidates?: JobberMatchCandidate[];
  selected_jobber_match_id?: string;
  create_jobber_expense?: boolean;
  upload_to_google_drive?: boolean;
}

export interface TagOption {
  id: string;
  name: string;
}

interface CustomField {
  id: number;
  name: string;
  data_type: string;
}

const DocumentProcessor: React.FC = () => {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedDocuments, setSelectedDocuments] = useState<number[]>([]);
  const [suggestions, setSuggestions] = useState<DocumentSuggestion[]>([]);
  const [availableTags, setAvailableTags] = useState<TagOption[]>([]);
  const [allCustomFields, setAllCustomFields] = useState<CustomField[]>([]);
  const [loading, setLoading] = useState(true);
  const [processing, setProcessing] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [isSuccessModalOpen, setIsSuccessModalOpen] = useState(false);
  const [filterTag, setFilterTag] = useState<string | null>(null);
  const [paperlessUrl, setPaperlessUrl] = useState<string>("");
  const [integrationStatuses, setIntegrationStatuses] = useState<Record<string, IntegrationStatus>>({});
  const [integrationResults, setIntegrationResults] = useState<DocumentIntegrationResult[]>([]);
  const [jobberEnabled, setJobberEnabled] = useState(true);
  const [jobberExpenseEnabled, setJobberExpenseEnabled] = useState(true);
  const [generateTitles, setGenerateTitles] = useState(true);
  const [generateTags, setGenerateTags] = useState(true);
  const [generateCorrespondents, setGenerateCorrespondents] = useState(true);
  const [generateDocumentTypes, setGenerateDocumentTypes] = useState(true);
  const [generateCreatedDate, setGenerateCreatedDate] = useState(true);
  const [generateCustomFields, setGenerateCustomFields] = useState(true);
  const [showAdvancedOptions, setShowAdvancedOptions] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSelectDocument = (docId: number) => {
    setSelectedDocuments((prev) =>
      prev.includes(docId) ? prev.filter((id) => id !== docId) : [...prev, docId]
    );
  };

  const handleSelectAll = () => setSelectedDocuments(documents.map((d) => d.id));
  const handleSelectNone = () => setSelectedDocuments([]);

  // Custom hook to fetch initial data
  const fetchInitialData = useCallback(async () => {
    try {
      const [filterTagRes, documentsRes, tagsRes, customFieldsRes, paperlessUrlRes, integrationsRes, settingsRes] = await Promise.all([
        axios.get<{ tag: string }>("./api/filter-tag"),
        axios.get<Document[]>("./api/documents"),
        axios.get<Record<string, number>>("./api/tags"),
        axios.get<CustomField[]>('./api/custom_fields'),
        axios.get<{ url: string }>("./api/paperless-url"),
        axios.get<{ providers: IntegrationStatus[] }>("./api/integrations"),
        axios.get<{ settings: { jobber_enabled?: boolean; jobber_expense_enabled?: boolean } }>("./api/settings"),
      ]);

      setFilterTag(filterTagRes.data.tag);
      setAllCustomFields(customFieldsRes.data || []);
      setPaperlessUrl(paperlessUrlRes.data.url || "");
      setIntegrationStatuses(
        Object.fromEntries(
          (integrationsRes.data.providers || []).map((provider) => [provider.provider, provider])
        )
      );
      setJobberEnabled(settingsRes.data.settings?.jobber_enabled ?? true);
      setJobberExpenseEnabled(settingsRes.data.settings?.jobber_expense_enabled ?? true);
      setDocuments(documentsRes.data);
      setSelectedDocuments(documentsRes.data.map((d: Document) => d.id));
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

  const handleProcessDocuments = async () => {
    setProcessing(true);
    setError(null);
    try {
      const docsToProcess = documents.filter((d) => selectedDocuments.includes(d.id));
      if (docsToProcess.length === 0) {
        setError("No documents selected. Please select at least one document to process.");
        setProcessing(false);
        return;
      }
      const requestPayload: GenerateSuggestionsRequest = {
        documents: docsToProcess,
        generate_titles: generateTitles,
        generate_tags: generateTags,
        generate_correspondents: generateCorrespondents,
        generate_document_types: generateDocumentTypes,
        generate_created_date: generateCreatedDate,
        generate_custom_fields: generateCustomFields,
      };

      const { data } = await axios.post<DocumentSuggestion[]>(
        "./api/generate-suggestions",
        requestPayload
      );

      // Post-process suggestions to add names and isSelected flag
      const customFieldMap = new Map((allCustomFields || []).map(cf => [cf.id, cf.name]));
      const processedSuggestions = data.map(suggestion => ({
        ...suggestion,
        suggested_custom_fields: suggestion.suggested_custom_fields?.map(cf => ({
          ...cf,
          name: customFieldMap.get(cf.id) || 'Unknown Field',
          isSelected: true,
        })),
        jobber_candidates: [],
        selected_jobber_match_id: suggestion.selected_jobber_match_id || "",
        create_jobber_expense: !!suggestion.create_jobber_expense,
        upload_to_google_drive: !!suggestion.upload_to_google_drive,
      }));

      setSuggestions(processedSuggestions);
      setIntegrationResults([]);

      if (integrationStatuses.jobber?.connected && jobberEnabled) {
        try {
          // Pass the full document objects so the server can rank candidates
          // without making extra Paperless API calls for each document.
          const jobberResponse = await axios.post<{ candidates: Record<string, JobberMatchCandidate[]> }>(
            "./api/integrations/jobber/match-candidates",
            {
              document_ids: docsToProcess.map((d) => d.id),
              documents: docsToProcess,
            }
          );

          setSuggestions((current) =>
            current.map((suggestion) => ({
              ...suggestion,
              jobber_candidates: jobberResponse.data.candidates?.[String(suggestion.id)] || [],
            }))
          );
        } catch (jobberError) {
          console.error("Error fetching Jobber candidates:", jobberError);
          let detail = "";
          if (axios.isAxiosError(jobberError) && jobberError.response?.data) {
            const data = jobberError.response.data as { code?: string; error?: string };
            if (data.code === "jobber_not_connected") {
              detail =
                " Jobber shows as disconnected — open Settings → Integrations and reconnect it.";
            } else if (data.code === "jobber_auth_failed") {
              detail =
                " Jobber rejected our credentials — please reconnect Jobber from Settings → Integrations.";
            } else if (typeof data.error === "string" && data.error.trim() !== "") {
              detail = ` (${data.error})`;
            }
          }
          setError(
            "Suggestions generated, but Jobber candidates could not be loaded." +
              detail
          );
        }
      }
    } catch (err) {
      console.error("Error generating suggestions:", err);
      setError("Failed to generate suggestions.");
    } finally {
      setProcessing(false);
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

      const response = await axios.patch<{ results: DocumentIntegrationResult[] }>("./api/update-documents", payload);
      setIntegrationResults(response.data.results || []);
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

  const handleJobberSelectionChange = (docId: number, selectedJobberMatchId: string) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId
          ? {
              ...doc,
              selected_jobber_match_id: selectedJobberMatchId,
              create_jobber_expense: selectedJobberMatchId ? doc.create_jobber_expense : false,
            }
          : doc
      )
    );
  };

  const handleJobberExpenseToggle = (docId: number, enabled: boolean) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId ? { ...doc, create_jobber_expense: enabled } : doc
      )
    );
  };

  const handleGoogleDriveToggle = (docId: number, enabled: boolean) => {
    setSuggestions((prevSuggestions) =>
      prevSuggestions.map((doc) =>
        doc.id === docId ? { ...doc, upload_to_google_drive: enabled } : doc
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

  const handleDeleteDocument = async (docId: number) => {
    try {
      await axios.delete(`./api/documents/${docId}`);
      setDocuments((prev) => prev.filter((d) => d.id !== docId));
      setSelectedDocuments((prev) => prev.filter((id) => id !== docId));
      setSuggestions((prev) => {
        const next = prev.filter((s) => s.id !== docId);
        return next;
      });
    } catch (err) {
      console.error("Error deleting document:", err);
      setError("Failed to delete document.");
    }
  };

  const reloadDocuments = async () => {
    setLoading(true);
    setError(null);
    try {
      const { data } = await axios.get<Document[]>("./api/documents");
      setDocuments(data);
      setSelectedDocuments(data.map((d: Document) => d.id));
    } catch (err) {
      console.error("Error reloading documents:", err);
      setError("Failed to reload documents.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (documents.length > 0) return;

    let delay = 5000;
    const maxDelay = 60000;
    let timeoutId: ReturnType<typeof setTimeout>;

    const poll = async () => {
      if (document.visibilityState === "hidden") {
        timeoutId = setTimeout(poll, delay);
        return;
      }
      try {
        const { data } = await axios.get<Document[]>("./api/documents");
        if (data.length > 0) {
          setDocuments(data);
          setSelectedDocuments(data.map((d: Document) => d.id));
          return;
        }
      } catch (err) {
        console.error("Error polling documents:", err);
      }
      delay = Math.min(delay * 2, maxDelay);
      timeoutId = setTimeout(poll, delay);
    };

    timeoutId = setTimeout(poll, delay);
    return () => clearTimeout(timeoutId);
  }, [documents]);

  if (loading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center bg-white dark:bg-gray-900">
        <div className="text-xl font-semibold text-gray-800 dark:text-gray-200">
          Loading documents...
        </div>
      </div>
    );
  }

  const generationOptions = [
    {
      label: "Titles",
      description: "Create clearer document titles.",
      checked: generateTitles,
      onChange: setGenerateTitles,
    },
    {
      label: "Tags",
      description: "Suggest tags for easier filing.",
      checked: generateTags,
      onChange: setGenerateTags,
    },
    {
      label: "Correspondents",
      description: "Fill in the sender or source.",
      checked: generateCorrespondents,
      onChange: setGenerateCorrespondents,
    },
    {
      label: "Document Types",
      description: "Classify the document type.",
      checked: generateDocumentTypes,
      onChange: setGenerateDocumentTypes,
    },
    {
      label: "Created Date",
      description: "Extract a likely document date.",
      checked: generateCreatedDate,
      onChange: setGenerateCreatedDate,
    },
    {
      label: "Custom Fields",
      description: "Populate matching custom fields.",
      checked: generateCustomFields,
      onChange: setGenerateCustomFields,
    },
  ];

  return (
    <div className="mx-auto max-w-6xl bg-white p-6 text-gray-800 dark:bg-gray-900 dark:text-gray-200">
      <header className="mb-8 rounded-3xl border border-gray-200 bg-gradient-to-br from-white via-blue-50 to-white p-6 shadow-sm dark:border-gray-800 dark:from-gray-900 dark:via-gray-800 dark:to-gray-900">
        <div className="flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
          <div className="max-w-3xl">
            <p className="text-sm font-semibold uppercase tracking-[0.2em] text-blue-600 dark:text-blue-400">
              Home
            </p>
            <h1 className="mt-2 text-4xl font-bold text-gray-900 dark:text-gray-100">
              Paperless GPT
            </h1>
            <p className="mt-3 text-base leading-7 text-gray-600 dark:text-gray-300">
              Review incoming Paperless documents, choose the metadata you want
              generated, and apply suggestions only after a final review.
            </p>
            <div className="mt-4 flex flex-wrap gap-2 text-sm text-gray-600 dark:text-gray-300">
              <span className="rounded-full bg-white px-3 py-1 shadow-sm dark:bg-gray-800">
                1. Select documents
              </span>
              <span className="rounded-full bg-white px-3 py-1 shadow-sm dark:bg-gray-800">
                2. Generate suggestions
              </span>
              <span className="rounded-full bg-white px-3 py-1 shadow-sm dark:bg-gray-800">
                3. Review before applying
              </span>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="rounded-2xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-gray-700 dark:bg-gray-800">
              <p className="text-sm text-gray-500 dark:text-gray-400">Documents in queue</p>
              <p className="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">
                {documents.length}
              </p>
            </div>
            <div className="rounded-2xl border border-gray-200 bg-white px-4 py-3 shadow-sm dark:border-gray-700 dark:bg-gray-800">
              <p className="text-sm text-gray-500 dark:text-gray-400">Selected</p>
              <p className="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">
                {selectedDocuments.length}
              </p>
            </div>
          </div>
        </div>
      </header>

      {error && (
        <div className="mb-6 rounded-2xl border border-red-200 bg-red-50 p-4 text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200">
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
          paperlessUrl={paperlessUrl}
          onDeleteDocument={handleDeleteDocument}
        >
          <div className="mb-6 rounded-3xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div>
                <h2 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">
                  Documents to Process
                </h2>
                <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">
                  Select the documents you want to enrich, then choose how much
                  metadata Paperless GPT should generate.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button
                  onClick={reloadDocuments}
                  disabled={processing}
                  className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
                >
                  <ArrowPathIcon className="h-5 w-5" />
                  Reload
                </button>
                <button
                  onClick={handleProcessDocuments}
                  disabled={processing || selectedDocuments.length === 0}
                  className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-blue-700 dark:hover:bg-blue-800"
                >
                  {processing
                    ? "Processing..."
                    : `Generate Suggestions (${selectedDocuments.length})`}
                </button>
              </div>
            </div>

            <div className="mt-5 flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
              <div className="flex flex-wrap items-center gap-2">
                <span className="rounded-full bg-blue-50 px-3 py-1 text-sm font-medium text-blue-700 dark:bg-blue-900/50 dark:text-blue-200">
                  {selectedDocuments.length} selected
                </span>
                <button
                  onClick={handleSelectAll}
                  className="rounded-md bg-gray-200 px-3 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-gray-300 dark:bg-gray-700 dark:text-gray-200 dark:hover:bg-gray-600"
                >
                  Select All
                </button>
                <button
                  onClick={handleSelectNone}
                  className="rounded-md bg-gray-200 px-3 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-gray-300 dark:bg-gray-700 dark:text-gray-200 dark:hover:bg-gray-600"
                >
                  Select None
                </button>
              </div>

              <button
                type="button"
                onClick={() => setShowAdvancedOptions((prev) => !prev)}
                className="inline-flex items-center justify-center rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                {showAdvancedOptions ? "Hide advanced options" : "Show advanced options"}
              </button>
            </div>

            {showAdvancedOptions && (
              <div className="mt-4 rounded-2xl bg-gray-50 p-4 dark:bg-gray-800/80">
                <p className="text-sm font-semibold text-gray-900 dark:text-gray-100">
                  Generate the following metadata
                </p>
                <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                  {generationOptions.map((option) => (
                    <label
                      key={option.label}
                      className={`flex cursor-pointer items-start gap-3 rounded-2xl border px-4 py-3 transition ${
                        option.checked
                          ? "border-blue-300 bg-blue-50 dark:border-blue-700 dark:bg-blue-950/30"
                          : "border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900"
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={option.checked}
                        onChange={(e) => option.onChange(e.target.checked)}
                        className="mt-1 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                      />
                      <span>
                        <span className="block text-sm font-medium text-gray-900 dark:text-gray-100">
                          {option.label}
                        </span>
                        <span className="mt-1 block text-sm text-gray-600 dark:text-gray-300">
                          {option.description}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              </div>
            )}
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
          onJobberMatchChange={handleJobberSelectionChange}
          onJobberExpenseToggle={handleJobberExpenseToggle}
          onGoogleDriveToggle={handleGoogleDriveToggle}
          onBack={resetSuggestions}
          onUpdate={handleUpdateDocuments}
          updating={updating}
          paperlessUrl={paperlessUrl}
          onDeleteDocument={handleDeleteDocument}
          integrationStatuses={integrationStatuses}
          integrationResults={integrationResults}
          jobberEnabled={jobberEnabled}
          jobberExpenseEnabled={jobberExpenseEnabled}
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

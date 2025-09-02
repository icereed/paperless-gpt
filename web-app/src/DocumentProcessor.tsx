import axios from "axios";
import React, { useCallback, useEffect, useState } from "react";
import "react-tag-autocomplete/example/src/styles.css"; // Ensure styles are loaded
import DocumentsToProcess from "./components/DocumentsToProcess";
import NoDocuments from "./components/NoDocuments";
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
  generate_created_date?: boolean;
  generate_custom_fields?: boolean;
  selected_custom_field_ids?: number[];
  custom_field_write_mode?: string;
}

export interface CustomFieldSuggestion {
  id: number;
  value: any;
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
  suggested_created_date?: string;
  suggested_custom_fields?: CustomFieldSuggestion[];
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
  const [suggestions, setSuggestions] = useState<DocumentSuggestion[]>([]);
  const [availableTags, setAvailableTags] = useState<TagOption[]>([]);
  const [allCustomFields, setAllCustomFields] = useState<CustomField[]>([]);
  const [loading, setLoading] = useState(true);
  const [processing, setProcessing] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [isSuccessModalOpen, setIsSuccessModalOpen] = useState(false);
  const [filterTag, setFilterTag] = useState<string | null>(null);
  const [generateTitles, setGenerateTitles] = useState(true);
  const [generateTags, setGenerateTags] = useState(true);
  const [generateCorrespondents, setGenerateCorrespondents] = useState(true);
  const [generateCreatedDate, setGenerateCreatedDate] = useState(true);
  const [generateCustomFields, setGenerateCustomFields] = useState(true);
  const [error, setError] = useState<string | null>(null);

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
      setAllCustomFields(customFieldsRes.data);
      setDocuments(documentsRes.data);
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
      const requestPayload: GenerateSuggestionsRequest = {
        documents,
        generate_titles: generateTitles,
        generate_tags: generateTags,
        generate_correspondents: generateCorrespondents,
        generate_created_date: generateCreatedDate,
        generate_custom_fields: generateCustomFields,
      };

      const { data } = await axios.post<DocumentSuggestion[]>(
        "./api/generate-suggestions",
        requestPayload
      );

      // Validate that data is an array
      if (!Array.isArray(data)) {
        console.error('API response is not an array:', data);
        setError("Invalid response format from server.");
        return;
      }

      // Check if the response is empty
      if (data.length === 0) {
        console.warn('API returned empty suggestions array');
        setError("No suggestions were generated. This may be due to LLM API connectivity issues in the test environment.");
        return;
      }

      // Post-process suggestions to add names and isSelected flag
      const customFieldMap = new Map(allCustomFields.map(cf => [cf.id, cf.name]));
      const processedSuggestions = data.map(suggestion => ({
        ...suggestion,
        suggested_custom_fields: suggestion.suggested_custom_fields?.map(cf => ({
          ...cf,
          name: customFieldMap.get(cf.id) || 'Unknown Field',
          isSelected: true,
        })) || [],
      }));

      setSuggestions(processedSuggestions);
    } catch (err) {
      console.error("Error generating suggestions:", err);
      // Provide more detailed error information
      if (axios.isAxiosError(err)) {
        if (err.response) {
          setError(`Server error: ${err.response.status} - ${err.response.statusText}`);
        } else if (err.request) {
          setError("Network error: Unable to reach server.");
        } else {
          setError(`Request error: ${err.message}`);
        }
      } else {
        setError("Failed to generate suggestions.");
      }
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
          generateTitles={generateTitles}
          setGenerateTitles={setGenerateTitles}
          generateTags={generateTags}
          setGenerateTags={setGenerateTags}
          generateCorrespondents={generateCorrespondents}
          setGenerateCorrespondents={setGenerateCorrespondents}
          generateCreatedDate={generateCreatedDate}
          setGenerateCreatedDate={setGenerateCreatedDate}
          generateCustomFields={generateCustomFields}
          setGenerateCustomFields={setGenerateCustomFields}
          onProcess={handleProcessDocuments}
          processing={processing}
          onReload={reloadDocuments}
        />
      ) : (
        <SuggestionsReview
          suggestions={suggestions}
          availableTags={availableTags}
          onTitleChange={handleTitleChange}
          onTagAddition={handleTagAddition}
          onTagDeletion={handleTagDeletion}
          onCorrespondentChange={handleCorrespondentChange}
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

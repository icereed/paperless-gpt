import {
  Dialog,
  DialogTitle,
  Transition,
  TransitionChild,
} from "@headlessui/react";
import { ArrowPathIcon, CheckCircleIcon } from "@heroicons/react/24/outline";
import axios from "axios";
import React, { Fragment, useEffect, useState } from "react";
import { ReactTags } from "react-tag-autocomplete";
import "react-tag-autocomplete/example/src/styles.css"; // Ensure styles are loaded

interface Document {
  id: number;
  title: string;
  content: string;
  tags: string[];
  suggested_title?: string;
  suggested_tags?: { value: string; label: string }[];
}

type ApiDocument = Omit<Document, "suggested_tags"> & {
  suggested_tags?: string[];
};

const DocumentProcessor: React.FC = () => {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [availableTags, setAvailableTags] = useState<{ value: string; label: string }[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [processing, setProcessing] = useState<boolean>(false);
  const [updating, setUpdating] = useState<boolean>(false);
  const [successModalOpen, setSuccessModalOpen] = useState<boolean>(false);
  const [filterTag, setFilterTag] = useState<string | undefined>(undefined);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [filterTagResponse, documentsResponse, tagsResponse] =
          await Promise.all([
            axios.get("/api/filter-tag"),
            axios.get("/api/documents"),
            axios.get("/api/tags"),
          ]);

        setFilterTag(filterTagResponse.data?.tag);
        const rawDocuments = documentsResponse.data as ApiDocument[];
        const documents = rawDocuments.map((doc) => ({
          ...doc,
          suggested_tags: doc.tags.map((tag) => ({ value: tag, label: tag })),
        }));
        console.log(documents);
        setDocuments(documents);

        // Store available tags as objects with value and label
        // tagsResponse.data is a map of name to id
        const tags = Object.entries(tagsResponse.data).map(([name]) => ({
          value: name,
          label: name,
        }));
        setAvailableTags(tags);
      } catch (error) {
        console.error("Error fetching data:", error);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, []);

  const handleProcessDocuments = async () => {
    setProcessing(true);
    try {
      const apiDocuments: ApiDocument[] = documents.map((doc) => ({
        ...doc,
        suggested_tags: doc.suggested_tags?.map((tag) => tag.value) || [],
      }));

      const response = await axios.post<ApiDocument[]>("/api/generate-suggestions", apiDocuments);
      setDocuments(response.data.map((doc) => ({
        ...doc,
        suggested_tags: doc.suggested_tags?.map((tag) => ({ value: tag, label: tag })) || [],
      })));
    } catch (error) {
      console.error("Error generating suggestions:", error);
    } finally {
      setProcessing(false);
    }
  };

  const handleUpdateDocuments = async () => {
    setUpdating(true);
    try {
      const apiDocuments: ApiDocument[] = documents.map((doc) => ({
        ...doc,
        tags: [], // Remove tags from the API document
        suggested_tags: doc.suggested_tags?.map((tag) => tag.value) || [],
      }));
      await axios.patch("/api/update-documents", apiDocuments);
      setSuccessModalOpen(true);
    } catch (error) {
      console.error("Error updating documents:", error);
    } finally {
      setUpdating(false);
    }
  };

  const resetSuggestions = () => {
    const resetDocs = documents.map((doc) => ({
      ...doc,
      suggested_title: undefined,
      suggested_tags: [],
    }));
    setDocuments(resetDocs);
  };

  const fetchDocuments = async () => {
    try {
      const response = await axios.get("/api/documents"); // API endpoint to fetch documents
      setDocuments(response.data);
    } catch (error) {
      console.error("Error fetching documents:", error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (documents.length === 0) {
      const interval = setInterval(() => {
        fetchDocuments();
      }, 500);
      return () => clearInterval(interval);
    }
  }, [documents]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="text-xl font-semibold">Loading documents...</div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto p-6">
      <h1 className="text-4xl font-bold mb-8 text-center text-gray-800">
        Paperless GPT
      </h1>

      {documents.length === 0 && (
        <div className="flex items-center justify-center h-screen">
          <div className="text-xl font-semibold">
            No documents found with filter tag{" "}
            <span className="bg-blue-100 text-blue-800 text-sm font-medium me-2 px-2.5 py-0.5 rounded dark:bg-blue-900 dark:text-blue-300bg-blue-100 text-blue-800 text-xs font-medium me-2 px-2.5 py-0.5 rounded-full dark:bg-blue-900 dark:text-blue-300">
              {filterTag}
            </span>{" "}
            found. Try{" "}
            <button
              onClick={() => {
                setDocuments([]);
                setLoading(true);
                fetchDocuments();
              }}
              className="text-blue-600 hover:underline focus:outline-none"
            >
              reloading <ArrowPathIcon className="h-5 w-5 inline" />
            </button>
            .
          </div>
        </div>
      )}

      {!documents.some((doc) => doc.suggested_title) && (
        <div className="space-y-6">
          <div className="flex justify-between items-center">
            <h2 className="text-2xl font-semibold text-gray-700">
              Documents to Process
            </h2>
            <button
              onClick={() => {
                setDocuments([]);
                setLoading(true);
                fetchDocuments();
              }}
              disabled={processing}
              className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 focus:outline-none"
            >
              <ArrowPathIcon className="h-5 w-5 inline" />
            </button>
            <button
              onClick={handleProcessDocuments}
              disabled={processing}
              className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 focus:outline-none"
            >
              {processing ? "Processing..." : "Generate Suggestions"}
            </button>
          </div>
          <div className="bg-white shadow rounded-md overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">
                    ID
                  </th>
                  <th className="px-6 py-3 text-left text-sm font-medium text-gray-500">
                    Title
                  </th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {documents.map((doc) => (
                  <tr key={doc.id}>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                      {doc.id}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                      {doc.title}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {documents.some((doc) => doc.suggested_title) && (
        <div className="space-y-6">
          <h2 className="text-2xl font-semibold text-gray-700">
            Review and Edit Suggested Titles
          </h2>
          <div className="bg-white shadow rounded-md overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-2 text-left text-sm font-medium text-gray-500">
                    ID
                  </th>
                  <th className="px-4 py-2 text-left text-sm font-medium text-gray-500">
                    Original Title
                  </th>
                  <th className="px-4 py-2 text-left text-sm font-medium text-gray-500">
                    Suggested Title
                  </th>
                  <th className="px-4 py-2 text-left text-sm font-medium text-gray-500">
                    Suggested Tags
                  </th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {documents.map(
                  (doc) =>
                    doc.suggested_title && (
                      <tr key={doc.id}>
                        <td className="px-4 py-3 text-sm text-gray-500">
                          {doc.id}
                        </td>
                        <td className="px-4 py-3 text-sm text-gray-900">
                          {doc.title}
                        </td>
                        <td className="px-4 py-3 text-sm text-gray-900">
                          <input
                            type="text"
                            value={doc.suggested_title}
                            onChange={(e) => {
                              const updatedDocuments = documents.map((d) =>
                                d.id === doc.id
                                  ? { ...d, suggested_title: e.target.value }
                                  : d
                              );
                              setDocuments(updatedDocuments);
                            }}
                            className="w-full border border-gray-300 rounded px-2 py-1 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          />
                        </td>
                        <td className="px-4 py-3 text-sm text-gray-900">
                          <ReactTags
                            selected={doc.suggested_tags || []}
                            suggestions={availableTags}

                            onAdd={(tag) => {
                              const updatedTags = [...(doc.suggested_tags || []), { value: tag.value as string, label: tag.label }];
                              const updatedDocuments = documents.map((d) =>
                                d.id === doc.id
                                  ? { ...d, suggested_tags: updatedTags }
                                  : d
                              );
                              setDocuments(updatedDocuments);
                            }}
                            onDelete={(i) => {
                              const updatedTags = doc.suggested_tags?.filter(
                                (_, index) => index !== i
                              );
                              const updatedDocuments = documents.map((d) =>
                                d.id === doc.id
                                  ? { ...d, suggested_tags: updatedTags }
                                  : d
                              );
                              setDocuments(updatedDocuments);
                            }}
                            allowNew={false}
                            placeholderText="Add a tag"
                          />
                        </td>
                      </tr>
                    )
                )}
              </tbody>
            </table>
          </div>
          <div className="flex justify-end space-x-4">
            <button
              onClick={resetSuggestions}
              className="bg-gray-200 text-gray-700 px-4 py-2 rounded hover:bg-gray-300 focus:outline-none"
            >
              Back
            </button>
            <button
              onClick={handleUpdateDocuments}
              disabled={updating}
              className={`${
                updating ? "bg-green-400" : "bg-green-600 hover:bg-green-700"
              } text-white px-4 py-2 rounded focus:outline-none`}
            >
              {updating ? "Updating..." : "Apply Suggestions"}
            </button>
          </div>
        </div>
      )}

      <Transition show={successModalOpen} as={Fragment}>
        <Dialog
          as="div"
          static
          className="fixed z-10 inset-0 overflow-y-auto"
          open={successModalOpen}
          onClose={setSuccessModalOpen}
        >
          <div className="flex items-end justify-center min-h-screen pt-4 px-4 pb-20 text-center sm:block sm:p-0">
            <TransitionChild
              as="div"
              enter="ease-out duration-300"
              enterFrom="opacity-0"
              enterTo="opacity-100"
              leave="ease-in duration-200"
              leaveFrom="opacity-100"
              leaveTo="opacity-0"
            >
              <div className="fixed inset-0 bg-gray-500 bg-opacity-75 transition-opacity" />
            </TransitionChild>

            <span
              className="hidden sm:inline-block sm:align-middle sm:h-screen"
              aria-hidden="true"
            >
              &#8203;
            </span>

            <TransitionChild
              as={Fragment}
              enter="ease-out duration-300"
              enterFrom="opacity-0 translate-y-4 sm:translate-y-0 sm:scale-95"
              enterTo="opacity-100 translate-y-0 sm:scale-100"
              leave="ease-in duration-200"
              leaveFrom="opacity-100 translate-y-0 sm:scale-100"
              leaveTo="opacity-0 translate-y-4 sm:translate-y-0 sm:scale-95"
            >
              <div className="inline-block align-bottom bg-white rounded-lg px-6 pt-5 pb-4 text-left overflow-hidden shadow-xl transform transition-all sm:align-middle sm:max-w-lg sm:w-full sm:p-6">
                <div className="sm:flex sm:items-start">
                  <div className="mx-auto flex-shrink-0 flex items-center justify-center h-12 w-12 rounded-full bg-green-100 sm:mx-0 sm:h-12 sm:w-12">
                    <CheckCircleIcon
                      className="h-6 w-6 text-green-600"
                      aria-hidden="true"
                    />
                  </div>
                  <div className="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left">
                    <DialogTitle
                      as="h3"
                      className="text-lg leading-6 font-medium text-gray-900"
                    >
                      Documents Updated
                    </DialogTitle>
                    <div className="mt-2">
                      <p className="text-sm text-gray-500">
                        The documents have been successfully updated with the
                        new titles.
                      </p>
                    </div>
                  </div>
                </div>
                <div className="mt-5 sm:mt-4 sm:flex sm:flex-row-reverse">
                  <button
                    onClick={() => {
                      setSuccessModalOpen(false);
                      setDocuments([]);
                      setLoading(true);
                      axios.get("/api/documents").then((response) => {
                        setDocuments(response.data);
                        setLoading(false);
                      });
                    }}
                    className="w-full inline-flex justify-center rounded-md border border-transparent shadow-sm px-4 py-2 bg-green-600 text-base font-medium text-white hover:bg-green-700 focus:outline-none sm:ml-3 sm:w-auto sm:text-sm"
                  >
                    OK
                  </button>
                </div>
              </div>
            </TransitionChild>
          </div>
        </Dialog>
      </Transition>
    </div>
  );
};

export default DocumentProcessor;
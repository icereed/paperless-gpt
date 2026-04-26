import React, { useMemo } from "react";
import { DocumentSuggestion, DocumentIntegrationResult, IntegrationStatus, TagOption } from "../DocumentProcessor";
import SuggestionCard from "./SuggestionCard";

interface SuggestionsReviewProps {
  suggestions: DocumentSuggestion[];
  availableTags: TagOption[];
  integrationStatuses: Record<string, IntegrationStatus>;
  integrationResults: DocumentIntegrationResult[];
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: TagOption) => void;
  onTagDeletion: (docId: number, index: number) => void;
  onCorrespondentChange: (docId: number, correspondent: string) => void;
  onDocumentTypeChange: (docId: number, documentType: string) => void;
  onCreatedDateChange: (docId: number, createdDate: string) => void;
  onCustomFieldSuggestionToggle: (docId: number, fieldId: number) => void;
  onJobberMatchChange: (docId: number, candidateId: string) => void;
  onApplyJobberToggle: (docId: number, checked: boolean) => void;
  onJobberExpenseToggle: (docId: number, checked: boolean) => void;
  onGoogleDriveToggle: (docId: number, checked: boolean) => void;
  onFireflyMatchChange: (docId: number, candidateId: string) => void;
  onApplyFireflyToggle: (docId: number, checked: boolean) => void;
  onFireflyCreateToggle: (docId: number, checked: boolean) => void;
  onQuickBooksToggle: (docId: number, checked: boolean) => void;
  onRegenerateSuggestion: (docId: number) => void;
  regeneratingDocId?: number | null;
  onBack: () => void;
  onUpdate: () => void;
  updating: boolean;
  paperlessUrl?: string;
  onDeleteDocument?: (documentId: number) => void;
  jobberEnabled?: boolean;
  jobberExpenseEnabled?: boolean;
  fireflyEnabled?: boolean;
  quickBooksReceiptUploadEnabled?: boolean;
}

const SuggestionsReview: React.FC<SuggestionsReviewProps> = ({
  suggestions,
  availableTags,
  integrationStatuses,
  integrationResults,
  onTitleChange,
  onTagAddition,
  onTagDeletion,
  onCorrespondentChange,
  onDocumentTypeChange,
  onCreatedDateChange,
  onCustomFieldSuggestionToggle,
  onJobberMatchChange,
  onApplyJobberToggle,
  onJobberExpenseToggle,
  onGoogleDriveToggle,
  onFireflyMatchChange,
  onApplyFireflyToggle,
  onFireflyCreateToggle,
  onQuickBooksToggle,
  onRegenerateSuggestion,
  regeneratingDocId,
  onBack,
  onUpdate,
  updating,
  paperlessUrl,
  onDeleteDocument,
  jobberEnabled = true,
  jobberExpenseEnabled = true,
  fireflyEnabled = false,
  quickBooksReceiptUploadEnabled = false,
}) => {
  const integrationResultMap = useMemo(
    () => new Map(integrationResults.map((r) => [r.document_id, r])),
    [integrationResults]
  );

  return (
  <section className="suggestions-review">
    <div className="mb-6 rounded-3xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="max-w-3xl">
          <p className="text-sm font-semibold uppercase tracking-[0.2em] text-green-600 dark:text-green-400">
            Review
          </p>
          <h2 className="mt-2 text-3xl font-bold text-gray-900 dark:text-gray-100">
            Edit suggestions before applying
          </h2>
          <p className="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
            Check each proposed title, tag, and metadata field before writing
            the updates back to Paperless.
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800">
            <p className="text-sm text-gray-500 dark:text-gray-400">Suggestions</p>
            <p className="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">
              {suggestions.length}
            </p>
          </div>
          <div className="rounded-2xl border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800">
            <p className="text-sm text-gray-500 dark:text-gray-400">Status</p>
            <p className="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">
              {updating ? "Applying updates..." : "Ready for review"}
            </p>
          </div>
        </div>
      </div>
    </div>

    <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
      {suggestions.map((doc) => (
        <SuggestionCard
          key={doc.id}
          suggestion={doc}
          availableTags={availableTags}
          onTitleChange={onTitleChange}
          onTagAddition={onTagAddition}
          onTagDeletion={onTagDeletion}
          onCorrespondentChange={onCorrespondentChange}
          onDocumentTypeChange={onDocumentTypeChange}
          onCreatedDateChange={onCreatedDateChange}
          onCustomFieldSuggestionToggle={onCustomFieldSuggestionToggle}
          onJobberMatchChange={onJobberMatchChange}
          onApplyJobberToggle={onApplyJobberToggle}
          onJobberExpenseToggle={onJobberExpenseToggle}
          onGoogleDriveToggle={onGoogleDriveToggle}
          onFireflyMatchChange={onFireflyMatchChange}
          onApplyFireflyToggle={onApplyFireflyToggle}
          onFireflyCreateToggle={onFireflyCreateToggle}
          onQuickBooksToggle={onQuickBooksToggle}
          onRegenerateSuggestion={onRegenerateSuggestion}
          regenerating={regeneratingDocId === doc.id}
          jobberConnected={!!integrationStatuses.jobber?.connected}
          jobberEnabled={jobberEnabled}
          jobberExpenseEnabled={jobberExpenseEnabled}
          fireflyConnected={!!integrationStatuses.firefly?.connected}
          fireflyEnabled={fireflyEnabled}
          quickBooksConnected={!!integrationStatuses.quickbooks?.connected}
          quickBooksReceiptUploadEnabled={quickBooksReceiptUploadEnabled}
          googleDriveConnected={!!integrationStatuses.google_drive?.connected}
          integrationResult={integrationResultMap.get(doc.id)}
          paperlessUrl={paperlessUrl}
          onDelete={onDeleteDocument}
        />
      ))}
    </div>

    <div className="mt-6 flex flex-col gap-3 rounded-3xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900 sm:flex-row sm:items-center sm:justify-between">
      <p className="text-sm text-gray-600 dark:text-gray-300">
        Go back to adjust document selection, or apply the reviewed suggestions
        when you are ready.
      </p>
      <div className="flex flex-wrap justify-end gap-3">
        <button
          onClick={onBack}
          className="rounded-md bg-gray-200 px-4 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-300 focus:outline-none dark:bg-gray-700 dark:text-gray-200 dark:hover:bg-gray-600"
        >
          Back
        </button>
        <button
          onClick={onUpdate}
          disabled={updating}
          className={`${
            updating
              ? "cursor-not-allowed bg-green-400 dark:bg-green-600"
              : "bg-green-600 hover:bg-green-700 dark:bg-green-700 dark:hover:bg-green-800"
          } rounded-md px-4 py-2 text-sm font-medium text-white focus:outline-none`}
        >
          {updating ? "Updating..." : "Apply Suggestions"}
        </button>
      </div>
    </div>
  </section>
  );
};

export default SuggestionsReview;

import React, { useMemo, useState, useCallback } from "react";
import ArrowTopRightOnSquareIcon from "@heroicons/react/24/outline/ArrowTopRightOnSquareIcon";
import EyeIcon from "@heroicons/react/24/outline/EyeIcon";
import TrashIcon from "@heroicons/react/24/outline/TrashIcon";
import { ReactTags } from "react-tag-autocomplete";
import { DocumentIntegrationResult, DocumentSuggestion, TagOption } from "../DocumentProcessor";
import DocumentPreviewModal from "./DocumentPreviewModal";

interface SuggestionCardProps {
  suggestion: DocumentSuggestion;
  availableTags: TagOption[];
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: TagOption) => void;
  onTagDeletion: (docId: number, index: number) => void;
  onCorrespondentChange: (docId: number, correspondent: string) => void;
  onDocumentTypeChange: (docId: number, documentType: string) => void;
  onCreatedDateChange: (docId: number, createdDate: string) => void;
  onCustomFieldSuggestionToggle: (docId: number, fieldId: number) => void;
  onJobberMatchChange: (docId: number, selectedJobId: string) => void;
  onApplyJobberToggle: (docId: number, enabled: boolean) => void;
  onGoogleDriveToggle: (docId: number, enabled: boolean) => void;
  onJobberExpenseToggle: (docId: number, enabled: boolean) => void;
  onRegenerateSuggestion?: (docId: number) => void;
  regenerating?: boolean;
  jobberConnected: boolean;
  jobberEnabled?: boolean;
  jobberExpenseEnabled?: boolean;
  googleDriveConnected: boolean;
  integrationResult?: DocumentIntegrationResult;
  paperlessUrl?: string;
  onDelete?: (documentId: number) => void;
}

const SuggestionCard: React.FC<SuggestionCardProps> = ({
  suggestion,
  availableTags,
  onTitleChange,
  onTagAddition,
  onTagDeletion,
  onCorrespondentChange,
  onDocumentTypeChange,
  onCreatedDateChange,
  onCustomFieldSuggestionToggle,
  onJobberMatchChange,
  onApplyJobberToggle,
  onGoogleDriveToggle,
  onJobberExpenseToggle,
  onRegenerateSuggestion,
  regenerating = false,
  jobberConnected,
  jobberEnabled = true,
  jobberExpenseEnabled = true,
  googleDriveConnected,
  integrationResult,
  paperlessUrl,
  onDelete,
}) => {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);
  const [jobberSearch, setJobberSearch] = useState("");
  const sortedAvailableTags = useMemo(
    () => [...availableTags].sort((a, b) => a.name.localeCompare(b.name)),
    [availableTags]
  );
  const document = suggestion.original_document;

  const filteredJobberCandidates = useMemo(() => {
    const q = jobberSearch.trim().toLowerCase();
    if (!q) return suggestion.jobber_candidates ?? [];
    return (suggestion.jobber_candidates ?? []).filter(
      (c) =>
        c.job_number?.toLowerCase().includes(q) ||
        c.client_name?.toLowerCase().includes(q) ||
        c.job_name?.toLowerCase().includes(q)
    );
  }, [suggestion.jobber_candidates, jobberSearch]);

  const selectedJobberCandidate = useMemo(
    () =>
      suggestion.jobber_candidates?.find(
        (c) => c.id === suggestion.selected_jobber_match_id
      ),
    [suggestion.jobber_candidates, suggestion.selected_jobber_match_id]
  );
  const jobberCandidatesAvailable = (suggestion.jobber_candidates?.length ?? 0) > 0;
  const applyJobberEnabled = !!suggestion.apply_jobber;

  const handleJobberSearchChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setJobberSearch(e.target.value);
  }, []);

  const handleDeleteClick = () => {
    if (confirmDelete) {
      if (onDelete) {
        onDelete(suggestion.id);
      }
    } else {
      setConfirmDelete(true);
    }
  };

  return (
    <>
      <div className="relative flex h-full flex-col justify-between rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => setIsPreviewOpen(true)}
            className="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-700 transition-colors hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-100 dark:hover:bg-gray-600"
          >
            <EyeIcon className="h-3.5 w-3.5" />
            Preview
          </button>
          {paperlessUrl && (
            <a
              href={`${paperlessUrl}/documents/${suggestion.id}/details`}
              target="_blank"
              rel="noopener noreferrer"
              title="View in Paperless-ngx"
              className="inline-flex items-center gap-1 rounded-md bg-blue-100 px-2.5 py-1 text-xs font-medium text-blue-700 transition-colors hover:bg-blue-200 dark:bg-blue-900 dark:text-blue-300 dark:hover:bg-blue-800"
            >
              <ArrowTopRightOnSquareIcon className="h-3.5 w-3.5" />
              View
            </a>
          )}
          {onDelete &&
            (confirmDelete ? (
              <>
                <button
                  onClick={handleDeleteClick}
                  title="Confirm delete"
                  className="inline-flex items-center gap-1 rounded-md bg-red-600 px-2.5 py-1 text-xs font-medium text-white transition-colors hover:bg-red-700"
                >
                  <TrashIcon className="h-3.5 w-3.5" />
                  Sure?
                </button>
                <button
                  onClick={() => setConfirmDelete(false)}
                  title="Cancel"
                  className="rounded-md bg-gray-200 px-2.5 py-1 text-xs font-medium text-gray-700 transition-colors hover:bg-gray-300 dark:bg-gray-600 dark:text-gray-200 dark:hover:bg-gray-500"
                >
                  Cancel
                </button>
              </>
            ) : (
              <button
                onClick={handleDeleteClick}
                title="Delete document"
                className="inline-flex items-center gap-1 rounded-md bg-red-100 px-2.5 py-1 text-xs font-medium text-red-700 transition-colors hover:bg-red-200 dark:bg-red-900 dark:text-red-300 dark:hover:bg-red-800"
              >
                <TrashIcon className="h-3.5 w-3.5" />
                Delete
              </button>
            ))}
        </div>

        <div className="mb-4 flex flex-wrap items-center gap-2">
          <span
            className={`rounded-full px-2.5 py-1 text-xs font-medium ${
              suggestion.cached
                ? "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-200"
                : "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-200"
            }`}
          >
            {suggestion.cached ? "Cached suggestion" : "Fresh suggestion"}
          </span>
          {suggestion.generated_at && (
            <span className="text-xs text-gray-500 dark:text-gray-400">
              Generated {new Date(suggestion.generated_at).toLocaleString()}
            </span>
          )}
          {onRegenerateSuggestion && (
            <button
              type="button"
              onClick={() => onRegenerateSuggestion(suggestion.id)}
              disabled={regenerating}
              className="rounded-md bg-purple-100 px-2.5 py-1 text-xs font-medium text-purple-700 transition-colors hover:bg-purple-200 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-purple-900 dark:text-purple-200 dark:hover:bg-purple-800"
            >
              {regenerating ? "Regenerating..." : "Regenerate"}
            </button>
          )}
        </div>

        <div>
          <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
            {document.title}
          </h3>
          <p className="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">
            {document.content.length > 120
              ? `${document.content.substring(0, 120)}...`
              : document.content || "No OCR text available yet."}
          </p>
          <div className="mt-4 flex flex-wrap gap-2">
            {document.tags.length > 0 ? (
              document.tags.map((tag) => (
                <span
                  key={tag}
                  className="rounded-full bg-blue-50 px-2.5 py-1 text-xs font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-200"
                >
                  {tag}
                </span>
              ))
            ) : (
              <span className="text-xs text-gray-500 dark:text-gray-400">
                No tags yet
              </span>
            )}
          </div>
        </div>

        <div className="mt-4">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
            Suggested Title
          </label>
          <input
            type="text"
            value={suggestion.suggested_title || ""}
            onChange={(e) => onTitleChange(suggestion.id, e.target.value)}
            className="mt-2 w-full rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
          />

          <div className="mt-4">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Suggested Tags
            </label>
            <ReactTags
              selected={
                suggestion.suggested_tags?.map((tag, index) => ({
                  id: index.toString(),
                  name: tag,
                  label: tag,
                  value: index.toString(),
                })) || []
              }
              suggestions={sortedAvailableTags.map((tag) => ({
                id: tag.id,
                name: tag.name,
                label: tag.name,
                value: tag.id,
              }))}
              onAdd={(tag) =>
                onTagAddition(suggestion.id, {
                  id: String(tag.value),
                  name: String(tag.label),
                })
              }
              onDelete={(index) => onTagDeletion(suggestion.id, index)}
              allowNew={true}
              placeholderText="Add a tag"
              classNames={{
                root: "react-tags dark:bg-gray-800",
                rootIsActive: "is-active",
                rootIsDisabled: "is-disabled",
                rootIsInvalid: "is-invalid",
                label: "react-tags__label",
                tagList: "react-tags__list",
                tagListItem: "react-tags__list-item",
                tag: "react-tags__tag dark:bg-blue-900 dark:text-blue-200",
                tagName: "react-tags__tag-name",
                comboBox: "react-tags__combobox dark:bg-gray-700 dark:text-gray-200",
                input: "react-tags__combobox-input dark:bg-gray-700 dark:text-gray-200",
                listBox: "react-tags__listbox dark:bg-gray-700 dark:text-gray-200",
                option: "react-tags__listbox-option dark:bg-gray-700 dark:text-gray-200 hover:bg-blue-500 dark:hover:bg-blue-800",
                optionIsActive: "is-active",
                highlight: "react-tags__highlight dark:bg-gray-800",
              }}
            />
          </div>

          <div className="mt-4">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Suggested Correspondent
            </label>
            <input
              type="text"
              value={suggestion.suggested_correspondent || ""}
              onChange={(e) => onCorrespondentChange(suggestion.id, e.target.value)}
              className="mt-2 w-full rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
              placeholder="Correspondent"
            />
          </div>

          <div className="mt-4">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Suggested Document Type
            </label>
            <input
              type="text"
              value={suggestion.suggested_document_type || ""}
              onChange={(e) => onDocumentTypeChange(suggestion.id, e.target.value)}
              className="mt-2 w-full rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
              placeholder="Document Type"
            />
          </div>

          <div className="mt-4">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Suggested Created Date
            </label>
            <input
              type="date"
              value={
                suggestion.suggested_created_date ||
                document.created_date ||
                ""
              }
              onChange={(e) => onCreatedDateChange(suggestion.id, e.target.value)}
              className="mt-2 w-full rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
            />
          </div>

          {suggestion.suggested_custom_fields && suggestion.suggested_custom_fields.length > 0 && (
            <div className="mt-4">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Suggested Custom Fields
              </label>
              <div className="mt-2 space-y-2">
                {suggestion.suggested_custom_fields?.map((field) => (
                  <div key={field.id} className="flex items-center">
                    <input
                      type="checkbox"
                      id={`custom-field-${suggestion.id}-${field.id}`}
                      checked={field.isSelected}
                      onChange={() => onCustomFieldSuggestionToggle(suggestion.id, field.id)}
                      className="mr-2 h-4 w-4"
                    />
                    <label
                      htmlFor={`custom-field-${suggestion.id}-${field.id}`}
                      className="text-sm"
                    >
                      <span className="font-semibold">{field.name}:</span>{" "}
                      {String(field.value)}
                    </label>
                  </div>
                )) || []}
              </div>
            </div>
          )}

          <div className="mt-6 space-y-4 border-t border-gray-200 pt-4 dark:border-gray-700">
            <div>
              <h4 className="text-sm font-semibold uppercase tracking-[0.12em] text-gray-500 dark:text-gray-400">
                Integrations for this document
              </h4>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                Choose which connected integrations should run when this suggestion is applied.
              </p>
            </div>

            <div className="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
              <input
                type="checkbox"
                id={`apply-jobber-${suggestion.id}`}
                checked={applyJobberEnabled}
                disabled={!jobberConnected || !jobberEnabled || !jobberCandidatesAvailable}
                onChange={(e) => onApplyJobberToggle(suggestion.id, e.target.checked)}
                className="mt-1 h-4 w-4"
              />
              <div>
                <label
                  htmlFor={`apply-jobber-${suggestion.id}`}
                  className="font-medium text-gray-700 dark:text-gray-300"
                >
                  Apply Jobber
                </label>
                <p className="text-sm text-gray-500 dark:text-gray-400">
                  {!jobberConnected
                    ? "Connect Jobber in Settings to enable job matching."
                    : !jobberEnabled
                      ? "Enable Jobber in Settings -> Integrations first."
                      : !jobberCandidatesAvailable
                        ? "No Jobber matches are available for this document."
                        : "Write the selected Jobber job to Paperless and optionally create an expense."}
                </p>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Jobber Match
                {(suggestion.jobber_candidates?.length ?? 0) > 0 && (
                  <span className="ml-1.5 text-xs font-normal text-gray-400 dark:text-gray-500">
                    ({suggestion.jobber_candidates!.length} jobs)
                  </span>
                )}
              </label>
              {applyJobberEnabled && jobberConnected && jobberEnabled && (suggestion.jobber_candidates?.length ?? 0) > 10 && (
                <input
                  type="text"
                  value={jobberSearch}
                  onChange={handleJobberSearchChange}
                  placeholder="Search by job #, client, or title…"
                  className="mt-1.5 w-full rounded border border-gray-300 px-2 py-1 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200 dark:placeholder-gray-400"
                />
              )}
              <select
                value={suggestion.selected_jobber_match_id || ""}
                onChange={(e) => onJobberMatchChange(suggestion.id, e.target.value)}
                disabled={!applyJobberEnabled || !jobberConnected || !jobberEnabled || !suggestion.jobber_candidates?.length}
                className="mt-1.5 w-full rounded border border-gray-300 px-2 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-60 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
                size={filteredJobberCandidates.length > 0 && jobberSearch ? Math.min(filteredJobberCandidates.length + 1, 8) : 1}
              >
                <option value="">
                  {!applyJobberEnabled
                    ? "Jobber is disabled for this document"
                    : !jobberConnected
                      ? "Connect Jobber in Settings"
                      : !jobberEnabled
                        ? "Enable Jobber in Settings -> Integrations"
                        : suggestion.jobber_candidates?.length
                          ? "-- No match --"
                          : "No Jobber matches found"}
                </option>
                {filteredJobberCandidates.map((candidate) => (
                  <option key={candidate.id} value={candidate.id}>
                    #{candidate.job_number} — {candidate.client_name} — {candidate.job_name}
                  </option>
                ))}
                {jobberSearch && filteredJobberCandidates.length === 0 && (
                  <option disabled value="">No results for "{jobberSearch}"</option>
                )}
              </select>
              {selectedJobberCandidate?.match_reason && (
                <p className="mt-1.5 text-xs text-blue-700 dark:text-blue-300">
                  {selectedJobberCandidate.match_reason}
                </p>
              )}
            </div>

            <div className="flex items-start gap-3">
              <input
                type="checkbox"
                id={`jobber-expense-${suggestion.id}`}
                checked={suggestion.create_jobber_expense ?? false}
                disabled={!applyJobberEnabled || !jobberConnected || !jobberEnabled || !suggestion.selected_jobber_match_id || !jobberExpenseEnabled}
                onChange={(e) => onJobberExpenseToggle(suggestion.id, e.target.checked)}
                className="mt-1 h-4 w-4"
              />
              <div>
                <label
                  htmlFor={`jobber-expense-${suggestion.id}`}
                  className="font-medium text-gray-700 dark:text-gray-300"
                >
                  Create Jobber expense
                </label>
                <p className="text-sm text-gray-500 dark:text-gray-400">
                  {!applyJobberEnabled
                    ? "Enable Apply Jobber for this document first."
                    : !jobberConnected
                    ? "Connect Jobber in Settings to enable expense creation."
                    : !jobberEnabled
                      ? "Enable Jobber in Settings -> Integrations first."
                      : !jobberExpenseEnabled
                        ? "Enable expense creation in Settings -> Integrations -> Jobber first."
                        : !suggestion.selected_jobber_match_id
                          ? "Select a Jobber job first."
                          : "Creates an expense linked to the selected Jobber job using the approved document details."}
                </p>
              </div>
            </div>

            <div className="flex items-start gap-3">
              <input
                type="checkbox"
                id={`google-drive-${suggestion.id}`}
                checked={suggestion.upload_to_google_drive ?? false}
                disabled={!googleDriveConnected}
                onChange={(e) => onGoogleDriveToggle(suggestion.id, e.target.checked)}
                className="mt-1 h-4 w-4"
              />
              <div>
                <label
                  htmlFor={`google-drive-${suggestion.id}`}
                  className="font-medium text-gray-700 dark:text-gray-300"
                >
                  Upload to Google Drive
                </label>
                <p className="text-sm text-gray-500 dark:text-gray-400">
                  {!googleDriveConnected
                    ? "Connect Google Drive in Settings to enable uploads."
                    : "Uploads the approved Paperless file to the configured folder."}
                </p>
              </div>
            </div>

            {(integrationResult?.jobber_applied ||
              integrationResult?.jobber_expense_created ||
              integrationResult?.jobber_error ||
              integrationResult?.jobber_expense_error ||
              integrationResult?.google_drive_uploaded ||
              integrationResult?.google_drive_error ||
              (integrationResult && integrationResult.paperless_updated && integrationResult.jobber_applied === false && !integrationResult.jobber_error)) && (
              <div className="rounded border border-gray-200 p-3 text-sm dark:border-gray-700">
                <h4 className="font-semibold text-gray-700 dark:text-gray-300">Last apply result</h4>
                {integrationResult?.jobber_applied && (
                  <p className="mt-1 text-green-700 dark:text-green-300">
                    Jobber fields saved to Paperless custom fields.
                  </p>
                )}
                {integrationResult && integrationResult.paperless_updated && integrationResult.jobber_applied === false && !integrationResult.jobber_error && (
                  <p className="mt-1 text-amber-700 dark:text-amber-300">
                    Jobber job was selected but no custom field mappings are configured - nothing was written to Paperless.
                    Go to <strong>Settings {"->"} Integrations {"->"} Jobber {"->"} Job matching</strong> to map Jobber fields to Paperless custom fields.
                  </p>
                )}
                {integrationResult?.jobber_expense_created && (
                  <p className="mt-1 text-green-700 dark:text-green-300">
                    Jobber expense created
                    {integrationResult.jobber_expense_id
                      ? ` (ID: ${integrationResult.jobber_expense_id})`
                      : "."}
                  </p>
                )}
                {integrationResult?.jobber_error && (
                  <p className="mt-1 text-red-700 dark:text-red-300">
                    Jobber field mapping error: {integrationResult.jobber_error}
                  </p>
                )}
                {integrationResult?.jobber_expense_error && (
                  <p className="mt-1 text-red-700 dark:text-red-300">
                    Jobber expense error: {integrationResult.jobber_expense_error}
                  </p>
                )}
                {integrationResult?.google_drive_uploaded && (
                  <p className="mt-1 text-green-700 dark:text-green-300">
                    Google Drive upload completed
                    {integrationResult.google_drive_url ? (
                      <>
                        {" "}
                        <a
                          className="underline"
                          href={integrationResult.google_drive_url}
                          target="_blank"
                          rel="noopener noreferrer"
                        >
                          Open file
                        </a>
                      </>
                    ) : "."}
                  </p>
                )}
                {integrationResult?.google_drive_error && (
                  <p className="mt-1 text-red-700 dark:text-red-300">
                    Google Drive: {integrationResult.google_drive_error}
                  </p>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      <DocumentPreviewModal
        isOpen={isPreviewOpen}
        onClose={() => setIsPreviewOpen(false)}
        title={document.title}
        content={document.content}
        correspondent={document.correspondent}
        tags={document.tags}
      />
    </>
  );
};

export default SuggestionCard;

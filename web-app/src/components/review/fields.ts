import { DocumentSuggestion } from "../../DocumentProcessor";

export type FieldKey =
  | "title"
  | "tags"
  | "correspondent"
  | "document_type"
  | "created_date";

export type Decision = "pending" | "applied" | "skipped" | "discarded";

export const fieldLabels: Record<FieldKey, string> = {
  title: "Title",
  tags: "Tags",
  correspondent: "Correspondent",
  document_type: "Document type",
  created_date: "Created date",
};

export const allFieldKeys = Object.keys(fieldLabels) as FieldKey[];

function normalizeTags(tags: string[] | undefined): string {
  return [...(tags || [])].sort().join("\0");
}

export function originalValue(
  suggestion: DocumentSuggestion,
  key: FieldKey
): string | string[] {
  const doc = suggestion.original_document;
  switch (key) {
    case "title":
      return doc.title;
    case "tags":
      return doc.tags;
    case "correspondent":
      return doc.correspondent;
    case "document_type":
      return doc.document_type_name || "";
    case "created_date":
      return doc.created_date || "";
  }
}

export function suggestedValue(
  suggestion: DocumentSuggestion,
  key: FieldKey
): string | string[] {
  switch (key) {
    case "title":
      return suggestion.suggested_title || "";
    case "tags":
      return suggestion.suggested_tags || [];
    case "correspondent":
      return suggestion.suggested_correspondent || "";
    case "document_type":
      return suggestion.suggested_document_type || "";
    case "created_date":
      return suggestion.suggested_created_date || "";
  }
}

/** A field counts as changed when a non-empty suggestion differs from the original. */
export function fieldChanged(
  suggestion: DocumentSuggestion,
  key: FieldKey
): boolean {
  if (key === "tags") {
    const suggested = suggestion.suggested_tags;
    if (!suggested || suggested.length === 0) return false;
    return normalizeTags(suggested) !== normalizeTags(suggestion.original_document.tags);
  }
  const suggested = suggestedValue(suggestion, key) as string;
  if (!suggested) return false;
  return suggested !== (originalValue(suggestion, key) as string);
}

/** Number of changes that would be applied for a document right now. */
export function countChanges(
  suggestion: DocumentSuggestion,
  excluded: Set<FieldKey>
): number {
  let count = allFieldKeys.filter(
    (key) => !excluded.has(key) && fieldChanged(suggestion, key)
  ).length;
  count +=
    suggestion.suggested_custom_fields?.filter((cf) => cf.isSelected).length ||
    0;
  return count;
}

/** Labels of the changes that would be applied, for the apply summary. */
export function changedFieldLabels(
  suggestion: DocumentSuggestion,
  excluded: Set<FieldKey>
): string[] {
  const labels = allFieldKeys
    .filter((key) => !excluded.has(key) && fieldChanged(suggestion, key))
    .map((key) => fieldLabels[key]);
  const customCount =
    suggestion.suggested_custom_fields?.filter((cf) => cf.isSelected).length ||
    0;
  if (customCount > 0) {
    labels.push(
      customCount === 1 ? "1 custom field" : `${customCount} custom fields`
    );
  }
  return labels;
}

/**
 * Build the update payload for a document: excluded fields are emptied so the
 * backend leaves them untouched (it skips empty suggestions), while everything
 * else on the suggestion (write modes, remove-tags) passes through unchanged.
 */
export function buildUpdatePayload(
  suggestion: DocumentSuggestion,
  excluded: Set<FieldKey>
): DocumentSuggestion {
  return {
    ...suggestion,
    suggested_title: excluded.has("title") ? "" : suggestion.suggested_title,
    suggested_tags: excluded.has("tags") ? [] : suggestion.suggested_tags,
    suggested_correspondent: excluded.has("correspondent")
      ? ""
      : suggestion.suggested_correspondent,
    suggested_document_type: excluded.has("document_type")
      ? ""
      : suggestion.suggested_document_type,
    suggested_created_date: excluded.has("created_date")
      ? ""
      : suggestion.suggested_created_date,
    // selectOption is only the label shown in the review, so it is left out
    // of the request; the option id in `value` is what gets written.
    suggested_custom_fields: suggestion.suggested_custom_fields
      ?.filter((cf) => cf.isSelected)
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      .map(({ selectOption, ...cf }) => cf),
  };
}

export interface SuggestionEditHandlers {
  onTitleChange: (docId: number, title: string) => void;
  onTagAddition: (docId: number, tag: { id: string; name: string }) => void;
  onTagDeletion: (docId: number, index: number) => void;
  onCorrespondentChange: (docId: number, correspondent: string) => void;
  onDocumentTypeChange: (docId: number, documentType: string) => void;
  onCreatedDateChange: (docId: number, createdDate: string) => void;
  onCustomFieldSuggestionToggle: (docId: number, fieldId: number) => void;
}

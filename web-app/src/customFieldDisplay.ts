/** A paperless-ngx custom field as returned by /api/custom_fields. */
export interface CustomFieldDefinition {
  id: number;
  name: string;
  data_type: string;
  extra_data?: {
    select_options?: { id: string; label: string }[] | null;
  } | null;
}

export interface SelectOptionDisplay {
  label: string;
  unknownOption: boolean;
}

/**
 * paperless-ngx stores a select field's value as the option's opaque id (e.g.
 * "dWrjErEPyl4ZkfeP"), which means nothing to a reviewer. Resolve it to the
 * option's label for display only; the id is still what gets written back.
 *
 * Returns null when the field is not a select field or has no value, so the
 * caller can fall back to its usual formatting. An id that matches no option
 * comes back as itself, flagged, so a bad suggestion is never hidden.
 */
export function selectOptionDisplay(
  field: CustomFieldDefinition | undefined,
  value: unknown
): SelectOptionDisplay | null {
  if (field?.data_type !== "select") return null;
  if (value === null || value === undefined || value === "") return null;

  const id = String(value);
  const option = field.extra_data?.select_options?.find((o) => o.id === id);
  return option
    ? { label: option.label, unknownOption: false }
    : { label: id, unknownOption: true };
}

import assert from "node:assert/strict";
import { it } from "node:test";
import type {
  CustomFieldSuggestion,
  DocumentSuggestion,
} from "../../DocumentProcessor";
import {
  buildUpdatePayload,
  customFieldInputError,
  hasInvalidCustomFields,
} from "./fields.ts";

function field(id: number, dataType: string, value: unknown): CustomFieldSuggestion {
  return { id, name: `Field ${id}`, dataType, value, isSelected: true };
}

function review(...fields: CustomFieldSuggestion[]): DocumentSuggestion {
  return {
    id: 1,
    original_document: { id: 1, title: "Example", content: "", tags: [], correspondent: "" },
    suggested_custom_fields: fields,
  };
}

it("sends a corrected value only for the selected field", () => {
  const invoiceNumber = field(1, "string", "DEMO-0047");
  const corrected = { ...invoiceNumber, value: "DEMO-0042" };
  const deselected = { ...field(2, "date", "2026-02-29"), isSelected: false };
  assert.deepEqual(buildUpdatePayload(review(corrected, deselected), new Set()).suggested_custom_fields, [
    { id: 1, name: "Field 1", value: "DEMO-0042", isSelected: true },
  ]);
});

it("blocks an invalid date only while selected", () => {
  const impossible = field(2, "date", "2026-02-29");
  assert.equal(hasInvalidCustomFields(review(impossible)), true);
  assert.equal(hasInvalidCustomFields(review({ ...impossible, isSelected: false })), false);
  assert.equal(customFieldInputError(field(2, "date", "2028-02-29")), undefined);
  assert.ok(customFieldInputError(field(2, "date", "0000-01-01")));
});

it("checks text length and leaves other field types to their existing handling", () => {
  assert.ok(customFieldInputError(field(1, "string", " ")));
  assert.ok(customFieldInputError(field(1, "string", "a".repeat(129))));
  assert.equal(customFieldInputError(field(1, "string", "😀".repeat(128))), undefined);
  assert.equal(customFieldInputError(field(3, "select", "example-option-id")), undefined);
});

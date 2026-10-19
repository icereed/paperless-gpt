import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  type CustomFieldDefinition,
  selectOptionDisplay,
} from "./customFieldDisplay.ts";

const who: CustomFieldDefinition = {
  id: 7,
  name: "Who",
  data_type: "select",
  extra_data: {
    select_options: [
      { id: "dWrjErEPyl4ZkfeP", label: "Alice" },
      { id: "HrlmdPvlS6blbh0j", label: "Bob" },
    ],
  },
};

describe("selectOptionDisplay", () => {
  it("shows the label of the option a select value names", () => {
    assert.deepEqual(selectOptionDisplay(who, "HrlmdPvlS6blbh0j"), {
      label: "Bob",
      unknownOption: false,
    });
  });

  it("flags an id that matches none of the options instead of hiding it", () => {
    assert.deepEqual(selectOptionDisplay(who, "notAnOptionId"), {
      label: "notAnOptionId",
      unknownOption: true,
    });
  });

  it("flags every id when the field has no options", () => {
    const noOptions = { ...who, extra_data: null };
    assert.deepEqual(selectOptionDisplay(noOptions, "HrlmdPvlS6blbh0j"), {
      label: "HrlmdPvlS6blbh0j",
      unknownOption: true,
    });
  });

  it("leaves other field types to their usual formatting", () => {
    const total: CustomFieldDefinition = {
      id: 1,
      name: "Total",
      data_type: "monetary",
    };
    assert.equal(selectOptionDisplay(total, "USD12.50"), null);
  });

  it("leaves an unknown field or an empty value to the usual formatting", () => {
    assert.equal(selectOptionDisplay(undefined, "HrlmdPvlS6blbh0j"), null);
    assert.equal(selectOptionDisplay(who, ""), null);
    assert.equal(selectOptionDisplay(who, null), null);
    assert.equal(selectOptionDisplay(who, undefined), null);
  });
});

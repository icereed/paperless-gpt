import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  clearReviewDecisions,
  filterRestoredSuggestions,
  parseReviewDecisions,
  recordReviewDecision,
  reviewDecisionsKey,
} from "./reviewSession.ts";

describe("parseReviewDecisions", () => {
  it("keeps only applied and skipped ids", () => {
    const decisions = parseReviewDecisions(
      JSON.stringify({
        "4": "applied",
        "5": "skipped",
        "6": "pending",
        "0": "applied",
        "-1": "skipped",
        no: "applied",
      })
    );
    assert.deepEqual(decisions, { 4: "applied", 5: "skipped" });
  });

  it("returns an empty map for missing or broken storage", () => {
    assert.deepEqual(parseReviewDecisions(null), {});
    assert.deepEqual(parseReviewDecisions("not-json"), {});
    assert.deepEqual(parseReviewDecisions("[]"), {});
  });
});

describe("filterRestoredSuggestions", () => {
  const suggestions = [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }];

  it("hides decided rows and rows that left the queue", () => {
    const kept = filterRestoredSuggestions(
      suggestions,
      new Set([1, 2, 4]),
      { 2: "skipped", 4: "applied" }
    );
    assert.deepEqual(kept, [{ id: 1 }]);
  });

  it("keeps an undecided document that is still queued", () => {
    const kept = filterRestoredSuggestions(suggestions, new Set([3]), {});
    assert.deepEqual(kept, [{ id: 3 }]);
  });
});

describe("recordReviewDecision", () => {
  it("writes one decision without dropping the others", () => {
    const values = new Map<string, string>();
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => {
        values.set(key, value);
      },
      removeItem: (key: string) => {
        values.delete(key);
      },
    };
    recordReviewDecision(storage, "job-1", 7, "applied");
    recordReviewDecision(storage, "job-1", 8, "skipped");
    assert.equal(
      values.get(reviewDecisionsKey("job-1")),
      JSON.stringify({ 7: "applied", 8: "skipped" })
    );
    clearReviewDecisions(storage, "job-1");
    assert.equal(values.has(reviewDecisionsKey("job-1")), false);
  });
});

export type StoredDecision = "applied" | "skipped";

export function reviewDecisionsKey(jobId: string): string {
  return `pgpt-suggestion-decisions:${jobId}`;
}

// parseReviewDecisions reads the decisions saved for one suggestion job.
// Anything that is not an applied/skipped entry for a positive document id
// is dropped, so a corrupted value cannot resurrect or hide the wrong rows.
export function parseReviewDecisions(
  raw: string | null
): Record<number, StoredDecision> {
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    const decisions: Record<number, StoredDecision> = {};
    for (const [key, value] of Object.entries(parsed)) {
      const id = Number(key);
      if (!Number.isInteger(id) || id <= 0) continue;
      if (value === "applied" || value === "skipped") {
        decisions[id] = value;
      }
    }
    return decisions;
  } catch {
    return {};
  }
}

export function recordReviewDecision(
  storage: Pick<Storage, "getItem" | "setItem">,
  jobId: string,
  documentId: number,
  decision: StoredDecision
): void {
  const decisions = parseReviewDecisions(
    storage.getItem(reviewDecisionsKey(jobId))
  );
  decisions[documentId] = decision;
  storage.setItem(reviewDecisionsKey(jobId), JSON.stringify(decisions));
}

export function clearReviewDecisions(
  storage: Pick<Storage, "removeItem">,
  jobId: string
): void {
  storage.removeItem(reviewDecisionsKey(jobId));
}

// filterRestoredSuggestions drops rows the user already decided and rows
// whose document has left the manual-review queue. A document leaves that
// queue when its suggestion is applied, including when the browser never
// heard about the success because the proxy closed a long request.
export function filterRestoredSuggestions<T extends { id: number }>(
  suggestions: T[],
  queuedIds: ReadonlySet<number>,
  decisions: Record<number, StoredDecision>
): T[] {
  return suggestions.filter((suggestion) => {
    const decision = decisions[suggestion.id];
    if (decision === "applied" || decision === "skipped") return false;
    return queuedIds.has(suggestion.id);
  });
}

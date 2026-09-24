package main

import (
	"context"
	"fmt"

	"paperless-gpt/extension"
)

// vocabularyCandidates returns the values offered to the LLM for a field:
// those of the registered Vocabulary if it restricts the field, otherwise the
// given paperless-ngx values unchanged.
func vocabularyCandidates(ctx context.Context, req extension.CandidatesRequest, existing []string) ([]string, error) {
	v := extension.VocabularyFor(req.Field)
	if v == nil {
		return existing, nil
	}
	candidates, err := v.Candidates(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s vocabulary unavailable: %w", req.Field, err)
	}
	if candidates.Unrestricted {
		return existing, nil
	}
	return candidates.Values, nil
}

// resolveVocabularyValue checks a proposed value against the Vocabulary
// registered for a field. Without a Vocabulary every value is accepted as-is.
// A Vocabulary error is returned so callers fail closed instead of writing an
// unchecked value.
func resolveVocabularyValue(ctx context.Context, req extension.ResolveRequest) (extension.Resolution, error) {
	v := extension.VocabularyFor(req.Field)
	if v == nil {
		return extension.Resolution{Accepted: true, Value: req.Proposed}, nil
	}
	resolution, err := v.Resolve(ctx, req)
	if err != nil {
		return extension.Resolution{}, fmt.Errorf("%s vocabulary could not check %q: %w", req.Field, req.Proposed, err)
	}
	return resolution, nil
}

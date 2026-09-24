package main

import (
	"context"
	"fmt"

	"paperless-gpt/extension"
)

// vocabularyCandidates returns the values offered to the LLM for a field:
// those of the registered Vocabulary if there is one, otherwise the given
// paperless-ngx values unchanged.
func vocabularyCandidates(ctx context.Context, field extension.Field, existing []string) ([]string, error) {
	v := extension.VocabularyFor(field)
	if v == nil {
		return existing, nil
	}
	candidates, err := v.Candidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s vocabulary unavailable: %w", field, err)
	}
	return candidates, nil
}

// resolveVocabularyValue checks a proposed value against the Vocabulary
// registered for a field. Without a Vocabulary every value is accepted as-is.
// A Vocabulary error is returned so callers fail closed instead of writing an
// unchecked value.
func resolveVocabularyValue(ctx context.Context, field extension.Field, proposed string) (extension.Resolution, error) {
	v := extension.VocabularyFor(field)
	if v == nil {
		return extension.Resolution{Accepted: true, Value: proposed}, nil
	}
	resolution, err := v.Resolve(ctx, proposed)
	if err != nil {
		return extension.Resolution{}, fmt.Errorf("%s vocabulary could not check %q: %w", field, proposed, err)
	}
	return resolution, nil
}

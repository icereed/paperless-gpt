package main

import (
	"context"
	"sort"
)

// extensionHost gives extensions read access to paperless-ngx through the
// configured client.
type extensionHost struct {
	client ClientInterface
}

func (h extensionHost) Correspondents(ctx context.Context) ([]string, error) {
	byName, err := h.client.GetAllCorrespondents(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (h extensionHost) DocumentTypes(ctx context.Context) ([]string, error) {
	types, err := h.client.GetAllDocumentTypes(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names, nil
}

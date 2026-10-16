package extension

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVocabulary struct{}

func (fakeVocabulary) Candidates(context.Context, CandidatesRequest) (Candidates, error) {
	return Candidates{}, nil
}
func (fakeVocabulary) Resolve(context.Context, ResolveRequest) (Resolution, error) {
	return Resolution{}, nil
}

type fakeExtension struct {
	name     string
	vars     []EnvVar
	startErr error
	started  *[]string
}

func (e fakeExtension) Name() string      { return e.name }
func (e fakeExtension) EnvVars() []EnvVar { return e.vars }
func (e fakeExtension) Start(context.Context) error {
	*e.started = append(*e.started, e.name)
	return e.startErr
}

func TestVocabularyRegistry(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	assert.Nil(t, VocabularyFor(FieldCorrespondent))

	RegisterVocabulary(FieldCorrespondent, fakeVocabulary{})
	assert.NotNil(t, VocabularyFor(FieldCorrespondent))

	assert.Panics(t, func() { RegisterVocabulary(FieldCorrespondent, fakeVocabulary{}) })
	assert.Panics(t, func() { RegisterVocabulary("other", nil) })
	assert.Nil(t, VocabularyFor("other"))
}

func TestStartRunsInOrderAndStopsAtFirstError(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	var started []string
	Register(fakeExtension{name: "a", vars: []EnvVar{{Name: "A"}}, started: &started})
	Register(fakeExtension{name: "b", startErr: errors.New("boom"), started: &started})
	Register(fakeExtension{name: "c", vars: []EnvVar{{Name: "C"}}, started: &started})

	err := Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extension b: boom")
	assert.Equal(t, []string{"a", "b"}, started)

	assert.Equal(t, []EnvVar{{Name: "A"}, {Name: "C"}}, EnvVars())
}

func TestNoExtensionsIsNoop(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	require.NoError(t, Start(context.Background()))
	assert.Empty(t, EnvVars())
	assert.Empty(t, Extensions())
}

type fakeHTTPExtension struct{ fakeExtension }

func (fakeHTTPExtension) Handler() http.Handler { return http.NotFoundHandler() }

func TestHTTPExtensions(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	var started []string
	Register(fakeExtension{name: "plain", started: &started})
	Register(fakeHTTPExtension{fakeExtension{name: "web", started: &started}})

	httpExts := HTTPExtensions()
	require.Len(t, httpExts, 1)
	assert.Equal(t, "web", httpExts[0].Name())
}

type fakeHost struct{}

func (fakeHost) Correspondents(context.Context) ([]string, error) { return []string{"Acme"}, nil }
func (fakeHost) DocumentTypes(context.Context) ([]string, error)  { return nil, nil }

func TestHost(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	assert.Nil(t, CurrentHost())
	SetHost(fakeHost{})
	names, err := CurrentHost().Correspondents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"Acme"}, names)
}

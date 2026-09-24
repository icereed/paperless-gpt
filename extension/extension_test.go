package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVocabulary struct{}

func (fakeVocabulary) Candidates(context.Context) ([]string, error) { return nil, nil }
func (fakeVocabulary) Resolve(context.Context, string) (Resolution, error) {
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

package ocr

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinerU_PollFailsImmediatelyOnPermanentError(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 410} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := newFakeMinerU(t)
			f.pollErrors = map[int]int{0: code, 1: code, 2: code}

			_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("mineru poll: HTTP %d", code))

			polls, cancels := f.counts()
			assert.Equal(t, 1, polls, "a permanent 4xx must not be polled again")
			assert.Equal(t, 0, cancels, "the server already rejected the job id")
		})
	}
}

func TestMinerU_PollRetriesTransientErrors(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := newFakeMinerU(t)
			f.jobStatuses = []string{"completed"}
			f.pollErrors = map[int]int{0: code, 1: code}

			res, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
			require.NoError(t, err)
			assert.Contains(t, res.Text, "Hello world.")

			polls, cancels := f.counts()
			assert.Equal(t, 3, polls)
			assert.Equal(t, 0, cancels)
		})
	}
}

func TestMinerU_PollGivesUpAfterThreeTransientErrors(t *testing.T) {
	f := newFakeMinerU(t)
	f.pollErrors = map[int]int{0: 429, 1: 429, 2: 429, 3: 429}

	_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 429")

	polls, _ := f.counts()
	assert.Equal(t, 3, polls)
}

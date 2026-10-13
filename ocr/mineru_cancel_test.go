package ocr

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinerU_CancelsJobOnTimeout(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running"}
	p := f.provider("")
	p.timeout = 100 * time.Millisecond

	_, err := p.ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	polls, cancels := f.counts()
	assert.Positive(t, polls)
	assert.Equal(t, 1, cancels, "abandoned job must be cancelled exactly once")
}

func TestMinerU_CancelsJobOnContextCancel(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	f.onPoll = func() { once.Do(cancel) }

	_, err := f.provider("").ProcessImage(ctx, testPDF, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	_, cancels := f.counts()
	assert.Equal(t, 1, cancels)
}

func TestMinerU_CancelSendsAPIKey(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running"}
	p := f.provider("sekret")
	p.timeout = 100 * time.Millisecond

	_, err := p.ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)

	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, "Bearer sekret", f.authSeen["DELETE /v1/parse/jobs/job_1"])
}

func TestMinerU_CancelFailureKeepsOriginalError(t *testing.T) {
	f := newFakeMinerU(t)
	f.jobStatuses = []string{"running"}
	f.cancelStatus = 500
	p := f.provider("")
	p.timeout = 100 * time.Millisecond

	_, err := p.ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotContains(t, err.Error(), "cancel")

	_, cancels := f.counts()
	assert.Equal(t, 1, cancels, "cancellation is best effort and not retried")
}

func TestMinerU_CancelsJobAfterRepeatedPollFailures(t *testing.T) {
	f := newFakeMinerU(t)
	f.pollErrors = map[int]int{0: 503, 1: 503, 2: 503}

	_, err := f.provider("").ProcessImage(context.Background(), testPDF, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.False(t, errors.Is(err, context.DeadlineExceeded))

	polls, cancels := f.counts()
	assert.Equal(t, 3, polls)
	assert.Equal(t, 1, cancels)
}

func TestMinerU_NoCancelForSettledJob(t *testing.T) {
	for _, st := range []string{"completed", "failed", "canceled", "partial", "bogus"} {
		t.Run(st, func(t *testing.T) {
			f := newFakeMinerU(t)
			f.jobStatuses = []string{"running", st}
			if st != "completed" {
				f.fileStatus = "failed"
			}
			_, _ = f.provider("").ProcessImage(context.Background(), testPDF, 0)
			_, cancels := f.counts()
			assert.Equal(t, 0, cancels)
		})
	}
}

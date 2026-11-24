package main

import (
	"testing"

	"paperless-gpt/ocr"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestInitLoggerPropagatesLevelToOCR checks that LOG_LEVEL also applies to the
// OCR package, which has its own logger.
func TestInitLoggerPropagatesLevelToOCR(t *testing.T) {
	origLogLevel := logLevel
	origMainLevel := log.GetLevel()
	origOCRLevel := ocr.GetLogLevel()
	t.Cleanup(func() {
		logLevel = origLogLevel
		log.SetLevel(origMainLevel)
		ocr.SetLogLevel(origOCRLevel)
	})

	tests := []struct {
		logLevel string
		want     logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"", logrus.InfoLevel},
	}

	for _, tt := range tests {
		t.Run("LOG_LEVEL="+tt.logLevel, func(t *testing.T) {
			logLevel = tt.logLevel
			initLogger()
			assert.Equal(t, tt.want, log.GetLevel(), "main logger")
			assert.Equal(t, tt.want, ocr.GetLogLevel(), "OCR logger")
		})
	}
}

package ocr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseHeaderList(t *testing.T) {
	got := ParseHeaderList(" A=1, B = two words ,C=x=y,broken,=nokey,D=")
	assert.Equal(t, map[string]string{"A": "1", "B": "two words", "C": "x=y", "D": ""}, got)
	assert.Empty(t, ParseHeaderList(""))
}

package ocr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinerU_URLValidationRejectsWithoutLeakingSecrets(t *testing.T) {
	cases := map[string]string{
		"no scheme with userinfo": "user:s3cret@mineru:8000",
		"empty host":              "http://user:s3cret@",
		"port too large":          "http://user:s3cret@host:99999999",
		"port zero":               "http://user:s3cret@host:0",
		"non-numeric port":        "http://user:s3cret@host:abc",
		"ftp scheme":              "ftp://user:s3cret@host",
		"file scheme":             "file:///s3cret",
		"query string":            "http://host:8000?token=s3cret",
		"empty query":             "http://user:s3cret@host:8000?",
		"fragment":                "http://host:8000/#s3cret",
		"empty fragment":          "http://user:s3cret@host:8000#",
		"bare host and port":      "mineru:8000",
		"garbage":                 "://s3cret",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newMinerUProvider(Config{MinerUURL: raw})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid MINERU_URL")
			assert.NotContains(t, err.Error(), "s3cret")

			_, err = NewProvider(Config{Provider: "mineru", MinerUURL: raw})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "s3cret")
		})
	}
}

func TestMinerU_URLValidationAccepts(t *testing.T) {
	for _, raw := range []string{
		"http://mineru:8000",
		"HTTPS://mineru.example",
		"http://10.0.0.5:65535/",
		"https://user:s3cret@proxy.example/mineru/v1/",
	} {
		p, err := newMinerUProvider(Config{MinerUURL: raw})
		require.NoError(t, err, raw)
		assert.NotEmpty(t, p.baseURL)
	}
}

func TestRedactURL_DropsQueryAndFragment(t *testing.T) {
	assert.Equal(t, "http://host:8000/mineru", redactURL("http://user:pw@host:8000/mineru?token=s3cret#frag"))
}

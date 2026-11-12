package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// paperless-ngx makes the request user the owner of a new object unless the
// body has an "owner" key, so the key itself is what matters, not its value.
func TestCreatedObjectOwner(t *testing.T) {
	tests := []struct {
		mode      string
		wantOwner bool // body must carry "owner": null
	}{
		{mode: "", wantOwner: false},
		{mode: CreatedObjectOwnerAPIUser, wantOwner: false},
		{mode: CreatedObjectOwnerNone, wantOwner: true},
	}

	for _, tt := range tests {
		t.Run("mode="+tt.mode, func(t *testing.T) {
			env := newTestEnv(t)
			defer env.teardown()
			env.client.CreatedObjectOwner = tt.mode

			posted := map[string]map[string]json.RawMessage{}
			capture := func(path, listBody string) {
				env.setMockResponse(path, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						w.WriteHeader(http.StatusOK)
						w.Write([]byte(listBody))
						return
					}
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					var fields map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(body, &fields))
					posted[path] = fields
					w.WriteHeader(http.StatusCreated)
					w.Write([]byte(`{"id": 42}`))
				})
			}
			capture("/api/tags/", `{"results": []}`)
			capture("/api/correspondents/", `{"results": []}`)

			ctx := context.Background()
			id, err := env.client.CreateTag(ctx, "new-tag")
			require.NoError(t, err)
			assert.Equal(t, 42, id)
			id, err = env.client.CreateOrGetCorrespondent(ctx, instantiateCorrespondent("New Corp"))
			require.NoError(t, err)
			assert.Equal(t, 42, id)

			for path, fields := range posted {
				owner, ok := fields["owner"]
				assert.Equal(t, tt.wantOwner, ok, "%s: owner key present", path)
				if ok {
					assert.JSONEq(t, "null", string(owner), path)
				}
			}
			assert.JSONEq(t, `"new-tag"`, string(posted["/api/tags/"]["name"]))
			assert.JSONEq(t, `"New Corp"`, string(posted["/api/correspondents/"]["name"]))
			assert.Contains(t, posted["/api/correspondents/"], "set_permissions")
		})
	}
}

func TestCreateObjectBodyKeepsExplicitOwner(t *testing.T) {
	client := NewPaperlessClient("http://example.com", "test-token")
	client.CreatedObjectOwner = CreatedObjectOwnerNone

	owner := 7
	corr := instantiateCorrespondent("Owned Corp")
	corr.Owner = &owner
	body, err := client.createObjectBody(corr)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	assert.JSONEq(t, "7", string(fields["owner"]))
}

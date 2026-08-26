package board

import (
	"context"
	"net/http"
	"testing"
)

// TestVerifierClientRound pins the service-role calls the GitHub Action
// verifier makes: pending fetch, keep (PATCH verified) and drop (DELETE).
func TestVerifierClientRound(t *testing.T) {
	var path, method string
	client, f := testClient(t, func(r *http.Request, _ string) (int, string) {
		path, method = r.URL.Path, r.Method
		switch {
		case r.Method == http.MethodGet:
			return 200, `[{"id":"id-1","name":"A","score":100,"level":2,"mode":"classic","day":null,"engine_version":"` + EngineVersion + `","replay":"{}"}]`
		case r.Method == http.MethodPatch:
			return 204, ""
		default:
			return 200, ""
		}
	})
	ctx := context.Background()

	pending, err := client.Pending(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != "id-1" || pending[0].EngineVersion != EngineVersion {
		t.Fatalf("pending = %+v", pending)
	}
	if q := f.last(t).URL.RawQuery; q == "" {
		t.Error("pending fetch missing filters")
	} else {
		for _, want := range []string{"verified=eq.false", "replay=not.is.null"} {
			if !contains(q, want) {
				t.Errorf("pending query %q missing %q", q, want)
			}
		}
	}

	if err := client.SetVerified(ctx, "id-1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || path != "/rest/v1/scores" {
		t.Errorf("SetVerified = %s %s", method, path)
	}
	if q := f.last(t).URL.RawQuery; q != "id=eq.id-1" {
		t.Errorf("SetVerified query = %q", q)
	}

	if err := client.DeleteRow(ctx, "id-1"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || path != "/rest/v1/scores" {
		t.Errorf("DeleteRow = %s %s", method, path)
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

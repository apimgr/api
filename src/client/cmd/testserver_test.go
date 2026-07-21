package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/output"
)

// recordedRequest captures what a command's Run function actually sent to
// the server, so tests can assert on the real path/query/body built by the
// command rather than re-implementing the URL construction logic.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

// newRecordingServer starts an httptest.Server that records the last
// request it received and replies with a fixed JSON body. It is closed
// automatically via t.Cleanup.
func newRecordingServer(t *testing.T, status int, respBody string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.Method = r.Method
		// EscapedPath (not Path) so percent-encoded segments like %2F
		// survive intact for tests that assert on URL-encoding behavior;
		// URL.Path would have already decoded them back to literal
		// characters.
		rec.Path = r.URL.EscapedPath()
		rec.Query = r.URL.Query()
		rec.Body = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// runCommand looks up category/name in the real registry and executes it
// against client, capturing whatever it printed to stdout via
// output.Capture so tests don't spam their own output.
func runCommand(t *testing.T, client *api.Client, category, name string, args []string) (string, error) {
	t.Helper()
	command, ok := findCommand(category, name)
	if !ok {
		t.Fatalf("command not registered: %s %s", category, name)
	}
	return output.Capture(func() error {
		return command.Run(client, &OutputOptions{Format: "json"}, args)
	})
}

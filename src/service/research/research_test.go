package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatCitationAPA(t *testing.T) {
	s := New()
	got := s.FormatCitationAPA("The Title", "Doe, J.", "2024", "Journal of Things")
	assert.Equal(t, "Doe, J.. (2024). The Title. Journal of Things", got)
}

func TestFormatCitationMLA(t *testing.T) {
	s := New()
	got := s.FormatCitationMLA("The Title", "Doe, J.", "Journal of Things", "2024")
	assert.Equal(t, `Doe, J.. "The Title." Journal of Things, 2024.`, got)
}

func TestFormatCitationChicago(t *testing.T) {
	s := New()
	got := s.FormatCitationChicago("Doe, J.", "The Title", "Journal of Things", "2024")
	assert.Equal(t, `Doe, J.. "The Title." Journal of Things (2024).`, got)
}

func TestGenerateBibliography(t *testing.T) {
	s := New()
	refs := []Reference{
		{Title: "Title One", Author: "Author One", Year: "2020", Source: "Source One"},
		{Title: "Title Two", Author: "Author Two", Year: "2021", Source: "Source Two"},
	}

	apa := s.GenerateBibliography(refs, "APA")
	assert.Len(t, apa, 2)
	assert.Equal(t, "Author One. (2020). Title One. Source One", apa[0])

	mla := s.GenerateBibliography(refs, "MLA")
	assert.Len(t, mla, 2)
	assert.Equal(t, `Author One. "Title One." Source One, 2020.`, mla[0])

	chicago := s.GenerateBibliography(refs, "Chicago")
	assert.Len(t, chicago, 2)
	assert.Equal(t, `Author One. "Title One." Source One (2020).`, chicago[0])

	// Unknown style falls back to a default format.
	def := s.GenerateBibliography(refs, "Unknown")
	assert.Len(t, def, 2)
	assert.Equal(t, "Author One - Title One (2020)", def[0])

	// Empty reference list yields an empty (nil) bibliography.
	empty := s.GenerateBibliography(nil, "APA")
	assert.Empty(t, empty)
}

func TestFormatDOI(t *testing.T) {
	s := New()
	assert.Equal(t, "https://doi.org/10.1000/xyz123", s.FormatDOI("10.1000/xyz123"))
}

func TestValidateDOI(t *testing.T) {
	s := New()
	assert.True(t, s.ValidateDOI("10.1000/xyz123"))
	assert.False(t, s.ValidateDOI("10.100"))
	assert.False(t, s.ValidateDOI("11.1000/xyz"))
	assert.False(t, s.ValidateDOI(""))
	assert.False(t, s.ValidateDOI("10"))
}

// redirectTransport rewrites every outbound request to target the given
// httptest server, regardless of the original host, so the package-level
// hardcoded endpoint constants can be exercised against a local mock.
type redirectTransport struct {
	targetURL *url.URL
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.targetURL.Scheme
	req.URL.Host = rt.targetURL.Host
	req.Host = rt.targetURL.Host
	return http.DefaultTransport.RoundTrip(req)
}

// withMockHTTPClient swaps the package-level httpClient for one that
// redirects all requests to srv, restoring the original on cleanup.
func withMockHTTPClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse test server url: %v", err)
	}
	original := httpClient
	httpClient = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &redirectTransport{targetURL: target},
	}
	t.Cleanup(func() {
		httpClient = original
	})
}

func TestArxivLookup(t *testing.T) {
	const validFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2101.00001v1</id>
    <title>  A Great Paper  </title>
    <summary>  A summary of the paper.  </summary>
    <published>2021-01-01T00:00:00Z</published>
    <updated>2021-01-02T00:00:00Z</updated>
    <author><name>Jane Doe</name></author>
    <author><name>John Roe</name></author>
    <link href="http://arxiv.org/abs/2101.00001v1" rel="alternate"/>
  </entry>
</feed>`

	const emptyFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"></feed>`

	const noTitleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2101.00001v1</id>
    <title></title>
  </entry>
</feed>`

	tests := []struct {
		name        string
		id          string
		useCanceled bool
		status      int
		body        string
		wantErr     bool
		wantTitle   string
		wantAuthors int
	}{
		{
			name:        "valid paper",
			id:          "2101.00001",
			status:      http.StatusOK,
			body:        validFeed,
			wantErr:     false,
			wantTitle:   "A Great Paper",
			wantAuthors: 2,
		},
		{
			name:    "empty id rejected",
			id:      "   ",
			wantErr: true,
		},
		{
			name:    "non-200 status",
			id:      "2101.00001",
			status:  http.StatusInternalServerError,
			body:    "",
			wantErr: true,
		},
		{
			name:    "malformed xml",
			id:      "2101.00001",
			status:  http.StatusOK,
			body:    "not xml at all <<<",
			wantErr: true,
		},
		{
			name:    "no entries",
			id:      "2101.00001",
			status:  http.StatusOK,
			body:    emptyFeed,
			wantErr: true,
		},
		{
			name:    "entry with empty title",
			id:      "2101.00001",
			status:  http.StatusOK,
			body:    noTitleFeed,
			wantErr: true,
		},
		{
			name:        "canceled context",
			id:          "2101.00001",
			useCanceled: true,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			withMockHTTPClient(t, srv)

			ctx := context.Background()
			if tt.useCanceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			s := New()
			result, err := s.ArxivLookup(ctx, tt.id)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				return
			}
			assert.NoError(t, err)
			if assert.NotNil(t, result) {
				assert.Equal(t, tt.wantTitle, result.Title)
				assert.Len(t, result.Authors, tt.wantAuthors)
				assert.Equal(t, "A summary of the paper.", result.Summary)
				assert.NotEmpty(t, result.Link)
			}
		})
	}
}

func TestISBNLookup(t *testing.T) {
	const validBody = `{
		"ISBN:0451526538": {
			"title": "The Adventures of Tom Sawyer",
			"publish_date": "2003",
			"number_of_pages": 275,
			"url": "https://openlibrary.org/books/OL1234M",
			"authors": [{"name": "Mark Twain"}],
			"publishers": [{"name": "Signet Classics"}]
		}
	}`

	const emptyBody = `{}`
	const noTitleBody = `{"ISBN:0000000000": {"title": ""}}`

	tests := []struct {
		name        string
		isbn        string
		useCanceled bool
		status      int
		body        string
		wantErr     bool
		wantTitle   string
		wantIsbn    string
		wantAuthors int
		wantPubs    int
	}{
		{
			name:        "valid isbn with dashes",
			isbn:        "0-451-52653-8",
			status:      http.StatusOK,
			body:        validBody,
			wantErr:     false,
			wantTitle:   "The Adventures of Tom Sawyer",
			wantIsbn:    "0451526538",
			wantAuthors: 1,
			wantPubs:    1,
		},
		{
			name:    "empty isbn rejected",
			isbn:    "   ",
			wantErr: true,
		},
		{
			name:    "non-200 status",
			isbn:    "0451526538",
			status:  http.StatusServiceUnavailable,
			body:    "",
			wantErr: true,
		},
		{
			name:    "malformed json",
			isbn:    "0451526538",
			status:  http.StatusOK,
			body:    "{not json",
			wantErr: true,
		},
		{
			name:    "no matching bibkey",
			isbn:    "0451526538",
			status:  http.StatusOK,
			body:    emptyBody,
			wantErr: true,
		},
		{
			name:    "matching key with empty title",
			isbn:    "0000000000",
			status:  http.StatusOK,
			body:    noTitleBody,
			wantErr: true,
		},
		{
			name:        "canceled context",
			isbn:        "0451526538",
			useCanceled: true,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			withMockHTTPClient(t, srv)

			ctx := context.Background()
			if tt.useCanceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			s := New()
			result, err := s.ISBNLookup(ctx, tt.isbn)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				return
			}
			assert.NoError(t, err)
			if assert.NotNil(t, result) {
				assert.Equal(t, tt.wantTitle, result.Title)
				assert.Equal(t, tt.wantIsbn, result.ISBN)
				assert.Len(t, result.Authors, tt.wantAuthors)
				assert.Len(t, result.Publishers, tt.wantPubs)
			}
		})
	}
}

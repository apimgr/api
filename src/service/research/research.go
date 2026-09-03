package research

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Service provides research utilities
type Service struct{}

// New creates a new Research service
func New() *Service {
	return &Service{}
}

// httpClient is a shared client with a hard timeout for the keyless arXiv
// and Open Library providers (no API key, free, fair-use rate limited)
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const (
	arxivEndpoint       = "https://export.arxiv.org/api/query"
	openLibraryEndpoint = "https://openlibrary.org/api/books"
)

// userAgent identifies this service to keyless providers, per Open
// Library's guidance to send a descriptive User-Agent with contact info
// for better rate limits on low-volume, non-bulk lookups.
const userAgent = "apimgr-api/1.0 (+https://github.com/apimgr/api)"

// ArxivResult is the response for an arXiv paper lookup
type ArxivResult struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Authors   []string `json:"authors"`
	Published string   `json:"published"`
	Updated   string   `json:"updated"`
	Link      string   `json:"link"`
}

// arxivFeed mirrors the Atom XML response from the arXiv query API
type arxivFeed struct {
	XMLName xml.Name `xml:"feed"`
	Entries []struct {
		ID        string `xml:"id"`
		Title     string `xml:"title"`
		Summary   string `xml:"summary"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
		Authors   []struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// ArxivLookup looks up an arXiv paper by its ID (e.g. "2101.00001") using
// the free, keyless arXiv query API.
func (s *Service) ArxivLookup(ctx context.Context, id string) (*ArxivResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("arxiv id is required")
	}

	endpoint := arxivEndpoint + "?id_list=" + url.QueryEscape(id) + "&max_results=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/atom+xml")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arxiv provider returned status %d", resp.StatusCode)
	}

	var feed arxivFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to decode arxiv provider response: %w", err)
	}
	if len(feed.Entries) == 0 {
		return nil, fmt.Errorf("no arxiv paper found for id %q", id)
	}

	entry := feed.Entries[0]
	result := &ArxivResult{
		ID:        strings.TrimSpace(entry.ID),
		Title:     strings.TrimSpace(entry.Title),
		Summary:   strings.TrimSpace(entry.Summary),
		Published: strings.TrimSpace(entry.Published),
		Updated:   strings.TrimSpace(entry.Updated),
	}
	for _, a := range entry.Authors {
		result.Authors = append(result.Authors, strings.TrimSpace(a.Name))
	}
	for _, l := range entry.Links {
		if l.Rel == "alternate" || l.Rel == "" {
			result.Link = l.Href
			break
		}
	}
	if result.Title == "" {
		return nil, fmt.Errorf("no arxiv paper found for id %q", id)
	}

	return result, nil
}

// ISBNResult is the response for an ISBN book lookup
type ISBNResult struct {
	ISBN        string   `json:"isbn"`
	Title       string   `json:"title"`
	Authors     []string `json:"authors,omitempty"`
	PublishDate string   `json:"publish_date,omitempty"`
	Publishers  []string `json:"publishers,omitempty"`
	PageCount   int      `json:"page_count,omitempty"`
	URL         string   `json:"url,omitempty"`
}

// openLibraryBook mirrors one entry in the Open Library Books API's
// jscmd=data response
type openLibraryBook struct {
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	PublishDate string `json:"publish_date"`
	NumberOf    int    `json:"number_of_pages"`
	URL         string `json:"url"`
	Authors     []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Publishers []struct {
		Name string `json:"name"`
	} `json:"publishers"`
}

// ISBNLookup looks up a book's metadata by ISBN using the free, keyless
// Open Library Books API. Open Library asks that low-volume, non-bulk
// callers send a descriptive User-Agent, which this method does.
func (s *Service) ISBNLookup(ctx context.Context, isbn string) (*ISBNResult, error) {
	isbn = strings.TrimSpace(strings.ReplaceAll(isbn, "-", ""))
	if isbn == "" {
		return nil, fmt.Errorf("isbn is required")
	}

	bibkey := "ISBN:" + isbn
	endpoint := openLibraryEndpoint + "?bibkeys=" + url.QueryEscape(bibkey) + "&format=json&jscmd=data"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open library request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open library returned status %d", resp.StatusCode)
	}

	var books map[string]openLibraryBook
	if err := json.NewDecoder(resp.Body).Decode(&books); err != nil {
		return nil, fmt.Errorf("failed to decode open library response: %w", err)
	}

	book, ok := books[bibkey]
	if !ok || book.Title == "" {
		return nil, fmt.Errorf("no book found for isbn %q", isbn)
	}

	result := &ISBNResult{
		ISBN:        isbn,
		Title:       book.Title,
		PublishDate: book.PublishDate,
		PageCount:   book.NumberOf,
		URL:         book.URL,
	}
	for _, a := range book.Authors {
		result.Authors = append(result.Authors, a.Name)
	}
	for _, p := range book.Publishers {
		result.Publishers = append(result.Publishers, p.Name)
	}

	return result, nil
}

// Citation formats
func (s *Service) FormatCitationAPA(title, author, year, source string) string {
	return fmt.Sprintf("%s. (%s). %s. %s", author, year, title, source)
}

func (s *Service) FormatCitationMLA(title, author, source, year string) string {
	return fmt.Sprintf("%s. \"%s.\" %s, %s.", author, title, source, year)
}

func (s *Service) FormatCitationChicago(author, title, source, year string) string {
	return fmt.Sprintf("%s. \"%s.\" %s (%s).", author, title, source, year)
}

// Bibliography generation
type Reference struct {
	Title  string
	Author string
	Year   string
	Source string
}

func (s *Service) GenerateBibliography(references []Reference, style string) []string {
	var bibliography []string

	for _, ref := range references {
		var citation string
		switch style {
		case "APA":
			citation = s.FormatCitationAPA(ref.Title, ref.Author, ref.Year, ref.Source)
		case "MLA":
			citation = s.FormatCitationMLA(ref.Title, ref.Author, ref.Source, ref.Year)
		case "Chicago":
			citation = s.FormatCitationChicago(ref.Author, ref.Title, ref.Source, ref.Year)
		default:
			citation = fmt.Sprintf("%s - %s (%s)", ref.Author, ref.Title, ref.Year)
		}
		bibliography = append(bibliography, citation)
	}

	return bibliography
}

// DOI utilities
func (s *Service) FormatDOI(doi string) string {
	return fmt.Sprintf("https://doi.org/%s", doi)
}

func (s *Service) ValidateDOI(doi string) bool {
	// Basic DOI format validation
	return len(doi) > 7 && doi[:3] == "10."
}

// Note: Full research service could include:
// 1. Citation extraction from text
// 2. Reference management
// 3. Academic database integration
// 4. Plagiarism detection

package research

import (
	"fmt"
)

// Service provides research utilities
type Service struct{}

// New creates a new Research service
func New() *Service {
	return &Service{}
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

package lorem

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// Service provides lorem ipsum and fake data generation
type Service struct{}

// New creates a new Lorem service
func New() *Service {
	return &Service{}
}

// Lorem ipsum text generation
var loremWords = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit",
	"sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore", "et", "dolore",
	"magna", "aliqua", "enim", "ad", "minim", "veniam", "quis", "nostrud", "exercitation",
	"ullamco", "laboris", "nisi", "aliquip", "ex", "ea", "commodo", "consequat",
}

func (s *Service) Words(count int) (string, error) {
	if count < 1 {
		count = 1
	}
	
	var words []string
	for i := 0; i < count; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(loremWords))))
		if err != nil {
			return "", err
		}
		words = append(words, loremWords[idx.Int64()])
	}
	
	return strings.Join(words, " "), nil
}

func (s *Service) Sentence(wordCount int) (string, error) {
	if wordCount < 1 {
		wordCount = 10
	}
	
	words, err := s.Words(wordCount)
	if err != nil {
		return "", err
	}
	
	// Capitalize first letter
	if len(words) > 0 {
		words = strings.ToUpper(string(words[0])) + words[1:]
	}
	
	return words + ".", nil
}

func (s *Service) Paragraph(sentenceCount int) (string, error) {
	if sentenceCount < 1 {
		sentenceCount = 5
	}
	
	var sentences []string
	for i := 0; i < sentenceCount; i++ {
		sentence, err := s.Sentence(10)
		if err != nil {
			return "", err
		}
		sentences = append(sentences, sentence)
	}
	
	return strings.Join(sentences, " "), nil
}

// Fake data generation
var firstNames = []string{
	"John", "Jane", "Michael", "Sarah", "David", "Emily", "Robert", "Jennifer",
	"William", "Linda", "Richard", "Patricia", "Joseph", "Barbara", "Thomas", "Elizabeth",
}

var lastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis",
	"Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Taylor",
}

func (s *Service) Person() (map[string]string, error) {
	firstIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(firstNames))))
	if err != nil {
		return nil, err
	}
	lastIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(lastNames))))
	if err != nil {
		return nil, err
	}
	
	firstName := firstNames[firstIdx.Int64()]
	lastName := lastNames[lastIdx.Int64()]
	
	return map[string]string{
		"name":  fmt.Sprintf("%s %s", firstName, lastName),
		"email": fmt.Sprintf("%s.%s@example.com", strings.ToLower(firstName), strings.ToLower(lastName)),
		"phone": fmt.Sprintf("+1-555-%04d", randomInt(10000)),
	}, nil
}

var cities = []string{
	"New York", "Los Angeles", "Chicago", "Houston", "Phoenix",
	"Philadelphia", "San Antonio", "San Diego", "Dallas", "San Jose",
}

var states = []string{
	"NY", "CA", "IL", "TX", "AZ", "PA", "TX", "CA", "TX", "CA",
}

func (s *Service) Address() (map[string]string, error) {
	cityIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(cities))))
	if err != nil {
		return nil, err
	}
	idx := cityIdx.Int64()
	
	return map[string]string{
		"street": fmt.Sprintf("%d Main St", randomInt(9999)+1),
		"city":   cities[idx],
		"state":  states[idx],
		"zip":    fmt.Sprintf("%05d", randomInt(99999)+1),
	}, nil
}

var companies = []string{
	"Tech Corp", "Digital Solutions", "Global Industries", "Innovation Labs",
	"Cloud Systems", "Data Dynamics", "Smart Services", "Future Tech",
}

var industries = []string{
	"Technology", "Healthcare", "Finance", "Manufacturing",
	"Retail", "Education", "Transportation", "Energy",
}

func (s *Service) Company() (map[string]string, error) {
	companyIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(companies))))
	if err != nil {
		return nil, err
	}
	industryIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(industries))))
	if err != nil {
		return nil, err
	}
	
	return map[string]string{
		"name":     companies[companyIdx.Int64()],
		"industry": industries[industryIdx.Int64()],
	}, nil
}

// Helper function
func randomInt(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

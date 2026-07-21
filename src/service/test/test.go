package test

import (
	"fmt"
	"strings"
	"time"
)

// Service provides testing utilities
type Service struct{}

// New creates a new Test service
func New() *Service {
	return &Service{}
}

// Test data generation
func (s *Service) GenerateTestEmail(prefix string) string {
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%s+test%d@example.com", prefix, timestamp)
}

func (s *Service) GenerateTestUsername(prefix string) string {
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%s_test_%d", prefix, timestamp)
}

// Mock data
func (s *Service) GenerateMockUser() map[string]interface{} {
	timestamp := time.Now().Unix()
	return map[string]interface{}{
		"id":       timestamp,
		"username": fmt.Sprintf("user_%d", timestamp),
		"email":    fmt.Sprintf("user%d@example.com", timestamp),
		"name":     "Test User",
		"active":   true,
		"created":  time.Now().Format(time.RFC3339),
	}
}

func (s *Service) GenerateMockAPIResponse() map[string]interface{} {
	return map[string]interface{}{
		"success":   true,
		"message":   "Test response",
		"data":      map[string]string{"test": "value"},
		"timestamp": time.Now().Format(time.RFC3339),
	}
}

// Assertion helpers
type TestResult struct {
	Passed  bool
	Message string
}

func (s *Service) AssertEqual(expected, actual interface{}) *TestResult {
	passed := fmt.Sprintf("%v", expected) == fmt.Sprintf("%v", actual)
	message := ""
	if !passed {
		message = fmt.Sprintf("Expected %v, got %v", expected, actual)
	}
	return &TestResult{Passed: passed, Message: message}
}

func (s *Service) AssertNotEqual(expected, actual interface{}) *TestResult {
	passed := fmt.Sprintf("%v", expected) != fmt.Sprintf("%v", actual)
	message := ""
	if !passed {
		message = fmt.Sprintf("Expected values to be different, both were %v", expected)
	}
	return &TestResult{Passed: passed, Message: message}
}

func (s *Service) AssertContains(haystack, needle string) *TestResult {
	passed := strings.Contains(haystack, needle)
	message := ""
	if !passed {
		message = fmt.Sprintf("Expected '%s' to contain '%s'", haystack, needle)
	}
	return &TestResult{Passed: passed, Message: message}
}

func (s *Service) AssertTrue(value bool) *TestResult {
	return &TestResult{
		Passed:  value,
		Message: "Expected true, got false",
	}
}

func (s *Service) AssertFalse(value bool) *TestResult {
	return &TestResult{
		Passed:  !value,
		Message: "Expected false, got true",
	}
}

// Benchmark helpers
func (s *Service) MeasureExecutionTime(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}

// Test fixtures
func (s *Service) GenerateFixture(fixtureType string) interface{} {
	switch fixtureType {
	case "user":
		return s.GenerateMockUser()
	case "api_response":
		return s.GenerateMockAPIResponse()
	default:
		return map[string]interface{}{"type": fixtureType, "test": true}
	}
}

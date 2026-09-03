package test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTestEmail(t *testing.T) {
	s := New()
	email := s.GenerateTestEmail("acme")
	assert.Regexp(t, `^acme\+test\d+@example\.com$`, email)
}

func TestGenerateTestUsername(t *testing.T) {
	s := New()
	username := s.GenerateTestUsername("acme")
	assert.Regexp(t, `^acme_test_\d+$`, username)
}

func TestGenerateMockUser(t *testing.T) {
	s := New()
	user := s.GenerateMockUser()

	require.Contains(t, user, "id")
	require.Contains(t, user, "username")
	require.Contains(t, user, "email")
	require.Contains(t, user, "name")
	require.Contains(t, user, "active")
	require.Contains(t, user, "created")

	assert.Equal(t, "Test User", user["name"])
	assert.Equal(t, true, user["active"])

	// created must be a valid RFC3339 timestamp.
	_, err := time.Parse(time.RFC3339, user["created"].(string))
	assert.NoError(t, err)
}

func TestGenerateMockAPIResponse(t *testing.T) {
	s := New()
	resp := s.GenerateMockAPIResponse()

	assert.Equal(t, true, resp["success"])
	assert.Equal(t, "Test response", resp["message"])
	assert.Equal(t, map[string]string{"test": "value"}, resp["data"])

	_, err := time.Parse(time.RFC3339, resp["timestamp"].(string))
	assert.NoError(t, err)
}

func TestAssertEqual(t *testing.T) {
	s := New()

	result := s.AssertEqual(1, 1)
	assert.True(t, result.Passed)
	assert.Empty(t, result.Message)

	result = s.AssertEqual(1, 2)
	assert.False(t, result.Passed)
	assert.NotEmpty(t, result.Message)

	// Compares via fmt string representation, so "1" and 1 are considered equal.
	result = s.AssertEqual("1", 1)
	assert.True(t, result.Passed)
}

func TestAssertNotEqual(t *testing.T) {
	s := New()

	result := s.AssertNotEqual(1, 2)
	assert.True(t, result.Passed)
	assert.Empty(t, result.Message)

	result = s.AssertNotEqual(1, 1)
	assert.False(t, result.Passed)
	assert.NotEmpty(t, result.Message)
}

func TestAssertContains(t *testing.T) {
	s := New()

	result := s.AssertContains("hello world", "world")
	assert.True(t, result.Passed)
	assert.Empty(t, result.Message)

	result = s.AssertContains("hello world", "xyz")
	assert.False(t, result.Passed)
	assert.NotEmpty(t, result.Message)

	// Empty needle is always contained.
	result = s.AssertContains("hello", "")
	assert.True(t, result.Passed)
}

func TestAssertTrueFalse(t *testing.T) {
	s := New()

	result := s.AssertTrue(true)
	assert.True(t, result.Passed)

	result = s.AssertTrue(false)
	assert.False(t, result.Passed)
	assert.Equal(t, "Expected true, got false", result.Message)

	result = s.AssertFalse(false)
	assert.True(t, result.Passed)

	result = s.AssertFalse(true)
	assert.False(t, result.Passed)
	assert.Equal(t, "Expected false, got true", result.Message)
}

func TestMeasureExecutionTime(t *testing.T) {
	s := New()

	called := false
	duration := s.MeasureExecutionTime(func() {
		called = true
		time.Sleep(5 * time.Millisecond)
	})

	assert.True(t, called)
	assert.GreaterOrEqual(t, duration, 5*time.Millisecond)
}

func TestGenerateFixture(t *testing.T) {
	s := New()

	userFixture := s.GenerateFixture("user")
	userMap, ok := userFixture.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, userMap, "username")

	apiFixture := s.GenerateFixture("api_response")
	apiMap, ok := apiFixture.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, apiMap, "success")

	unknownFixture := s.GenerateFixture("custom_type")
	unknownMap, ok := unknownFixture.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "custom_type", unknownMap["type"])
	assert.Equal(t, true, unknownMap["test"])
}

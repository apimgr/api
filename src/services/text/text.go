package text

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var rng *rand.Rand

func init() {
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
}

// UUID generates a UUID of the specified version
func UUID(version int) (string, error) {
	var id uuid.UUID
	var err error

	switch version {
	case 1:
		id, err = uuid.NewUUID()
	case 4:
		id = uuid.New()
	case 7:
		id, err = uuid.NewV7()
	default:
		id = uuid.New()
	}

	if err != nil {
		return "", err
	}

	return id.String(), nil
}

// UUIDs generates multiple UUIDs
func UUIDs(version, count int) ([]string, error) {
	if count <= 0 {
		count = 1
	}
	if count > 1000 {
		count = 1000
	}

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		id, err := UUID(version)
		if err != nil {
			return nil, err
		}
		uuids[i] = id
	}

	return uuids, nil
}

// Hash computes a hash of the input using the specified algorithm
func Hash(algorithm, input string) (string, error) {
	var h hash.Hash

	switch strings.ToLower(algorithm) {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha384":
		h = sha512.New384()
	case "sha512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashAll returns all common hashes
func HashAll(input string) map[string]string {
	hashes := make(map[string]string)

	algorithms := []string{"md5", "sha1", "sha256", "sha384", "sha512"}
	for _, alg := range algorithms {
		if h, err := Hash(alg, input); err == nil {
			hashes[alg] = h
		}
	}

	return hashes
}

// Base64Encode encodes input to base64
func Base64Encode(input string) string {
	return base64.StdEncoding.EncodeToString([]byte(input))
}

// Base64Decode decodes base64 input
func Base64Decode(input string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// Base64URLEncode encodes input to URL-safe base64
func Base64URLEncode(input string) string {
	return base64.URLEncoding.EncodeToString([]byte(input))
}

// Base64URLDecode decodes URL-safe base64 input
func Base64URLDecode(input string) (string, error) {
	decoded, err := base64.URLEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// Base32Encode encodes input to base32
func Base32Encode(input string) string {
	return base32.StdEncoding.EncodeToString([]byte(input))
}

// Base32Decode decodes base32 input
func Base32Decode(input string) (string, error) {
	decoded, err := base32.StdEncoding.DecodeString(input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// HexEncode encodes input to hexadecimal
func HexEncode(input string) string {
	return hex.EncodeToString([]byte(input))
}

// HexDecode decodes hexadecimal input
func HexDecode(input string) (string, error) {
	decoded, err := hex.DecodeString(input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// URLEncode encodes input for URL
func URLEncode(input string) string {
	return url.QueryEscape(input)
}

// URLDecode decodes URL-encoded input
func URLDecode(input string) (string, error) {
	return url.QueryUnescape(input)
}

// Case conversion
func ToLower(input string) string {
	return strings.ToLower(input)
}

func ToUpper(input string) string {
	return strings.ToUpper(input)
}

func ToTitle(input string) string {
	return strings.Title(strings.ToLower(input))
}

func ToCamelCase(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 {
		return ""
	}

	result := strings.ToLower(words[0])
	for _, word := range words[1:] {
		if len(word) > 0 {
			result += strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}

	return result
}

func ToSnakeCase(input string) string {
	words := strings.Fields(input)
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}
	return strings.Join(words, "_")
}

func ToKebabCase(input string) string {
	words := strings.Fields(input)
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}
	return strings.Join(words, "-")
}

// Reverse reverses a string
func Reverse(input string) string {
	runes := []rune(input)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// Stats returns text statistics
func Stats(input string) map[string]interface{} {
	lines := strings.Split(input, "\n")
	words := strings.Fields(input)

	return map[string]interface{}{
		"characters":          len(input),
		"characters_no_space": len(strings.ReplaceAll(input, " ", "")),
		"words":               len(words),
		"lines":               len(lines),
		"bytes":               len([]byte(input)),
	}
}

// ROT13 applies ROT13 cipher
func ROT13(input string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return 'a' + (r-'a'+13)%26
		case r >= 'A' && r <= 'Z':
			return 'A' + (r-'A'+13)%26
		}
		return r
	}, input)
}

// Lorem ipsum text
var loremWords = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit",
	"sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore", "et", "dolore",
	"magna", "aliqua", "enim", "ad", "minim", "veniam", "quis", "nostrud",
	"exercitation", "ullamco", "laboris", "nisi", "aliquip", "ex", "ea", "commodo",
	"consequat", "duis", "aute", "irure", "in", "reprehenderit", "voluptate",
	"velit", "esse", "cillum", "fugiat", "nulla", "pariatur", "excepteur", "sint",
	"occaecat", "cupidatat", "non", "proident", "sunt", "culpa", "qui", "officia",
	"deserunt", "mollit", "anim", "id", "est", "laborum",
}

// LoremWords generates random lorem ipsum words
func LoremWords(count int) []string {
	if count <= 0 {
		count = 10
	}
	if count > 1000 {
		count = 1000
	}

	words := make([]string, count)
	for i := 0; i < count; i++ {
		words[i] = loremWords[rng.Intn(len(loremWords))]
	}

	return words
}

// LoremSentences generates lorem ipsum sentences
func LoremSentences(count int) []string {
	if count <= 0 {
		count = 5
	}
	if count > 100 {
		count = 100
	}

	sentences := make([]string, count)
	for i := 0; i < count; i++ {
		wordCount := 8 + rng.Intn(10) // 8-17 words per sentence
		words := LoremWords(wordCount)
		words[0] = strings.Title(words[0])
		sentences[i] = strings.Join(words, " ") + "."
	}

	return sentences
}

// LoremParagraphs generates lorem ipsum paragraphs
func LoremParagraphs(count int) []string {
	if count <= 0 {
		count = 3
	}
	if count > 20 {
		count = 20
	}

	paragraphs := make([]string, count)
	for i := 0; i < count; i++ {
		sentenceCount := 3 + rng.Intn(4) // 3-6 sentences per paragraph
		sentences := LoremSentences(sentenceCount)
		paragraphs[i] = strings.Join(sentences, " ")
	}

	return paragraphs
}

package text

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"crypto/md5"
	crand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"html"
	"io"
	"math/big"
	"math/rand"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

// titleCaser is the non-deprecated replacement for strings.Title, per
// staticcheck SA1019 (golang.org/x/text/cases instead of strings.Title).
var titleCaser = cases.Title(language.Und)

func ToTitle(input string) string {
	return titleCaser.String(strings.ToLower(input))
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
		words[0] = titleCaser.String(words[0])
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

// machineID is a per-process identifier derived from the hostname, used to
// reduce collisions across processes for id formats that embed a machine id.
var machineID = computeMachineID()

func computeMachineID() []byte {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}
	sum := sha256.Sum256([]byte(hostname))
	return sum[:3]
}

// cuidCounter, xidCounter and objectIDCounter provide monotonically
// increasing per-process sequence numbers for their respective id formats.
var (
	cuidCounter     uint32
	xidCounter      uint32
	objectIDCounter uint32
	snowflakeSeq    uint32
)

// crockfordAlphabet is the Crockford base32 alphabet used by ULID.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// encodeULID encodes 16 bytes (128 bits) into the 26-character Crockford
// base32 representation used by ULID.
func encodeULID(b [16]byte) string {
	var dst [26]byte
	dst[0] = crockfordAlphabet[(b[0]&224)>>5]
	dst[1] = crockfordAlphabet[b[0]&31]
	dst[2] = crockfordAlphabet[(b[1]&248)>>3]
	dst[3] = crockfordAlphabet[((b[1]&7)<<2)|((b[2]&192)>>6)]
	dst[4] = crockfordAlphabet[(b[2]&62)>>1]
	dst[5] = crockfordAlphabet[((b[2]&1)<<4)|((b[3]&240)>>4)]
	dst[6] = crockfordAlphabet[((b[3]&15)<<1)|((b[4]&128)>>7)]
	dst[7] = crockfordAlphabet[(b[4]&124)>>2]
	dst[8] = crockfordAlphabet[((b[4]&3)<<3)|((b[5]&224)>>5)]
	dst[9] = crockfordAlphabet[b[5]&31]
	dst[10] = crockfordAlphabet[(b[6]&248)>>3]
	dst[11] = crockfordAlphabet[((b[6]&7)<<2)|((b[7]&192)>>6)]
	dst[12] = crockfordAlphabet[(b[7]&62)>>1]
	dst[13] = crockfordAlphabet[((b[7]&1)<<4)|((b[8]&240)>>4)]
	dst[14] = crockfordAlphabet[((b[8]&15)<<1)|((b[9]&128)>>7)]
	dst[15] = crockfordAlphabet[(b[9]&124)>>2]
	dst[16] = crockfordAlphabet[((b[9]&3)<<3)|((b[10]&224)>>5)]
	dst[17] = crockfordAlphabet[b[10]&31]
	dst[18] = crockfordAlphabet[(b[11]&248)>>3]
	dst[19] = crockfordAlphabet[((b[11]&7)<<2)|((b[12]&192)>>6)]
	dst[20] = crockfordAlphabet[(b[12]&62)>>1]
	dst[21] = crockfordAlphabet[((b[12]&1)<<4)|((b[13]&240)>>4)]
	dst[22] = crockfordAlphabet[((b[13]&15)<<1)|((b[14]&128)>>7)]
	dst[23] = crockfordAlphabet[(b[14]&124)>>2]
	dst[24] = crockfordAlphabet[((b[14]&3)<<3)|((b[15]&224)>>5)]
	dst[25] = crockfordAlphabet[b[15]&31]
	return string(dst[:])
}

// ULID generates a ULID: a 48-bit millisecond timestamp followed by 80 bits
// of cryptographically random entropy, encoded as 26 Crockford base32 chars.
func ULID() string {
	ts := uint64(time.Now().UnixMilli())

	var entropy [10]byte
	crand.Read(entropy[:])

	var b [16]byte
	b[0] = byte(ts >> 40)
	b[1] = byte(ts >> 32)
	b[2] = byte(ts >> 24)
	b[3] = byte(ts >> 16)
	b[4] = byte(ts >> 8)
	b[5] = byte(ts)
	copy(b[6:], entropy[:])

	return encodeULID(b)
}

// nanoIDAlphabet is the default alphabet used by NanoID (64 characters).
const nanoIDAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

// NanoID generates a 21-character NanoID using the default alphabet and
// cryptographically random bytes.
func NanoID() string {
	const size = 21
	raw := make([]byte, size)
	crand.Read(raw)

	id := make([]byte, size)
	for i, b := range raw {
		id[i] = nanoIDAlphabet[b&63]
	}

	return string(id)
}

// ksuidEpoch is the KSUID epoch: 2014-05-13T16:53:20Z.
const ksuidEpoch = 1400000000

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// KSUID generates a K-Sortable Unique Identifier: a 32-bit timestamp
// (seconds since the KSUID epoch) followed by 128 bits of random payload,
// base62-encoded to a fixed 27-character string.
func KSUID() string {
	ts := uint32(time.Now().Unix() - ksuidEpoch)

	payload := make([]byte, 16)
	crand.Read(payload)

	buf := make([]byte, 20)
	binary.BigEndian.PutUint32(buf[0:4], ts)
	copy(buf[4:], payload)

	n := new(big.Int).SetBytes(buf)
	base := big.NewInt(62)
	zero := big.NewInt(0)
	mod := new(big.Int)

	var chars []byte
	for n.Cmp(zero) > 0 {
		n.DivMod(n, base, mod)
		chars = append([]byte{base62Alphabet[mod.Int64()]}, chars...)
	}
	for len(chars) < 27 {
		chars = append([]byte{'0'}, chars...)
	}

	return string(chars)
}

const xidAlphabet = "0123456789abcdefghijklmnopqrstuv"

// encodeXID encodes the 12-byte XID payload into its 20-character
// lowercase base32-hex representation.
func encodeXID(id [12]byte) string {
	var dst [20]byte
	dst[0] = xidAlphabet[id[0]>>3]
	dst[1] = xidAlphabet[(id[1]>>6)&0x1F|(id[0]<<2)&0x1F]
	dst[2] = xidAlphabet[(id[1]>>1)&0x1F]
	dst[3] = xidAlphabet[(id[2]>>4)&0x1F|(id[1]<<4)&0x1F]
	dst[4] = xidAlphabet[id[3]>>7|(id[2]<<1)&0x1F]
	dst[5] = xidAlphabet[(id[3]>>2)&0x1F]
	dst[6] = xidAlphabet[id[4]>>5|(id[3]<<3)&0x1F]
	dst[7] = xidAlphabet[id[4]&0x1F]
	dst[8] = xidAlphabet[id[5]>>3]
	dst[9] = xidAlphabet[(id[6]>>6)&0x1F|(id[5]<<2)&0x1F]
	dst[10] = xidAlphabet[(id[6]>>1)&0x1F]
	dst[11] = xidAlphabet[(id[7]>>4)&0x1F|(id[6]<<4)&0x1F]
	dst[12] = xidAlphabet[id[8]>>7|(id[7]<<1)&0x1F]
	dst[13] = xidAlphabet[(id[8]>>2)&0x1F]
	dst[14] = xidAlphabet[id[9]>>5|(id[8]<<3)&0x1F]
	dst[15] = xidAlphabet[id[9]&0x1F]
	dst[16] = xidAlphabet[id[10]>>3]
	dst[17] = xidAlphabet[(id[11]>>6)&0x1F|(id[10]<<2)&0x1F]
	dst[18] = xidAlphabet[(id[11]>>1)&0x1F]
	dst[19] = xidAlphabet[(id[11]<<4)&0x1F]
	return string(dst[:])
}

// XID generates a globally unique 12-byte identifier: a 4-byte timestamp,
// a 3-byte machine id, a 2-byte process id, and a 3-byte counter.
func XID() string {
	var id [12]byte
	binary.BigEndian.PutUint32(id[0:4], uint32(time.Now().Unix()))
	copy(id[4:7], machineID)
	pid := os.Getpid()
	id[7] = byte(pid >> 8)
	id[8] = byte(pid)

	c := atomic.AddUint32(&xidCounter, 1)
	id[9] = byte(c >> 16)
	id[10] = byte(c >> 8)
	id[11] = byte(c)

	return encodeXID(id)
}

// CUID generates a collision-resistant identifier composed of a prefix, a
// base36 timestamp, a base36 counter, a fingerprint derived from the
// process, and a block of random data.
func CUID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

	toBase36 := func(n uint64, width int) string {
		if n == 0 {
			return strings.Repeat("0", width)
		}
		var chars []byte
		for n > 0 {
			chars = append([]byte{alphabet[n%36]}, chars...)
			n /= 36
		}
		s := string(chars)
		for len(s) < width {
			s = "0" + s
		}
		return s
	}

	timestamp := toBase36(uint64(time.Now().UnixMilli()), 8)
	counter := toBase36(uint64(atomic.AddUint32(&cuidCounter, 1)), 4)
	fingerprint := toBase36(uint64(binary.BigEndian.Uint16(machineID[0:2]))<<16|uint64(os.Getpid()&0xFFFF), 4)

	randBytes := make([]byte, 4)
	crand.Read(randBytes)
	randomBlock := toBase36(uint64(binary.BigEndian.Uint32(randBytes)), 8)

	return "c" + timestamp[len(timestamp)-8:] + counter + fingerprint[len(fingerprint)-4:] + randomBlock
}

// snowflakeEpoch is the Twitter Snowflake custom epoch (2010-11-04T01:42:54.657Z).
const snowflakeEpoch = int64(1288834974657)

// Snowflake generates a Twitter-style Snowflake id: a 41-bit millisecond
// timestamp, a 10-bit machine id, and a 12-bit per-millisecond sequence.
func Snowflake() string {
	ts := time.Now().UnixMilli() - snowflakeEpoch
	mid := int64(binary.BigEndian.Uint16(machineID[0:2])) & 0x3FF
	seq := int64(atomic.AddUint32(&snowflakeSeq, 1)) & 0xFFF

	id := (ts << 22) | (mid << 12) | seq
	return fmt.Sprintf("%d", id)
}

// ObjectID generates a MongoDB-style ObjectID: a 4-byte unix timestamp,
// a 5-byte random process value, and a 3-byte incrementing counter,
// hex-encoded to 24 characters.
func ObjectID() string {
	var b [12]byte
	binary.BigEndian.PutUint32(b[0:4], uint32(time.Now().Unix()))

	random := make([]byte, 5)
	crand.Read(random)
	copy(b[4:9], random)

	c := atomic.AddUint32(&objectIDCounter, 1)
	b[9] = byte(c >> 16)
	b[10] = byte(c >> 8)
	b[11] = byte(c)

	return hex.EncodeToString(b[:])
}

// Slugify converts input into a URL-safe slug: lowercase alphanumerics
// separated by single hyphens, with leading/trailing hyphens trimmed.
func Slugify(input string) string {
	lower := strings.ToLower(input)

	var sb strings.Builder
	lastDash := false
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			sb.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && sb.Len() > 0 {
				sb.WriteRune('-')
				lastDash = true
			}
		}
	}

	return strings.TrimRight(sb.String(), "-")
}

// ToPascalCase converts input to PascalCase.
func ToPascalCase(input string) string {
	words := strings.Fields(input)
	var sb strings.Builder
	for _, word := range words {
		if len(word) == 0 {
			continue
		}
		lw := strings.ToLower(word)
		sb.WriteString(strings.ToUpper(lw[:1]) + lw[1:])
	}
	return sb.String()
}

func ToScreamingSnake(input string) string { return strings.ToUpper(ToSnakeCase(input)) }
func ToDotCase(input string) string        { return strings.ReplaceAll(ToSnakeCase(input), "_", ".") }

// Count returns basic character/word/line counts.
func Count(input string) map[string]int {
	return map[string]int{"chars": len(input), "words": len(strings.Fields(input)), "lines": strings.Count(input, "\n") + 1}
}

// Diff produces a line-based unified-style diff between two texts using a
// longest-common-subsequence alignment. Lines are prefixed with "  " for
// unchanged, "- " for removed and "+ " for added.
func Diff(text1, text2 string) string {
	lines1 := strings.Split(text1, "\n")
	lines2 := strings.Split(text2, "\n")
	n, m := len(lines1), len(lines2)

	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if lines1[i] == lines2[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case lines1[i] == lines2[j]:
			out = append(out, "  "+lines1[i])
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "- "+lines1[i])
			i++
		default:
			out = append(out, "+ "+lines2[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+lines1[i])
	}
	for ; j < m; j++ {
		out = append(out, "+ "+lines2[j])
	}

	return strings.Join(out, "\n")
}

// Levenshtein computes the edit distance between two strings.
func Levenshtein(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	n, m := len(r1), len(r2)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}

	return prev[m]
}

// Similarity returns a 0.0-1.0 ratio derived from the Levenshtein distance,
// where 1.0 means identical strings.
func Similarity(s1, s2 string) float64 {
	if s1 == "" && s2 == "" {
		return 1.0
	}
	dist := Levenshtein(s1, s2)
	maxLen := len([]rune(s1))
	if l2 := len([]rune(s2)); l2 > maxLen {
		maxLen = l2
	}
	if maxLen == 0 {
		return 1.0
	}
	return 1.0 - float64(dist)/float64(maxLen)
}

var soundexCodes = map[rune]byte{
	'B': '1', 'F': '1', 'P': '1', 'V': '1',
	'C': '2', 'G': '2', 'J': '2', 'K': '2', 'Q': '2', 'S': '2', 'X': '2', 'Z': '2',
	'D': '3', 'T': '3',
	'L': '4',
	'M': '5', 'N': '5',
	'R': '6',
}

// Soundex computes the standard four-character Soundex phonetic code.
func Soundex(input string) string {
	runes := []rune(strings.ToUpper(strings.TrimSpace(input)))
	idx := 0
	for idx < len(runes) && !unicode.IsLetter(runes[idx]) {
		idx++
	}
	if idx >= len(runes) {
		return ""
	}

	first := runes[idx]
	result := []byte{byte(first)}
	lastCode := soundexCodes[first]

	for i := idx + 1; i < len(runes) && len(result) < 4; i++ {
		r := runes[i]
		if !unicode.IsLetter(r) {
			continue
		}
		// H and W do not break coalescing of surrounding same-code
		// consonants; only vowels (and Y) reset the previous code.
		if r == 'H' || r == 'W' {
			continue
		}
		code, ok := soundexCodes[r]
		if !ok {
			lastCode = 0
			continue
		}
		if code != lastCode {
			result = append(result, code)
		}
		lastCode = code
	}

	for len(result) < 4 {
		result = append(result, '0')
	}

	return string(result)
}

// Metaphone computes a simplified Metaphone phonetic code following the
// standard consonant/digraph rules of the Lawrence Philips algorithm.
func Metaphone(input string) string {
	var letters []rune
	for _, r := range strings.ToUpper(input) {
		if unicode.IsLetter(r) {
			letters = append(letters, r)
		}
	}
	n := len(letters)
	if n == 0 {
		return ""
	}

	isVowel := func(r rune) bool {
		switch r {
		case 'A', 'E', 'I', 'O', 'U':
			return true
		}
		return false
	}

	idx := 0
	switch {
	case n >= 2 && (string(letters[0:2]) == "AE" || string(letters[0:2]) == "GN" ||
		string(letters[0:2]) == "KN" || string(letters[0:2]) == "PN" || string(letters[0:2]) == "WR"):
		idx = 1
	case letters[0] == 'X':
		letters[0] = 'S'
	}

	var code strings.Builder
	var last rune

	for i := idx; i < n; i++ {
		c := letters[i]
		if c == last && c != 'C' {
			continue
		}

		var next, next2, prev rune
		if i+1 < n {
			next = letters[i+1]
		}
		if i+2 < n {
			next2 = letters[i+2]
		}
		if i > 0 {
			prev = letters[i-1]
		}

		switch c {
		case 'A', 'E', 'I', 'O', 'U':
			if i == idx {
				code.WriteRune(c)
			}
		case 'B':
			if !(i == n-1 && prev == 'M') {
				code.WriteRune('B')
			}
		case 'C':
			switch {
			case next == 'I' && next2 == 'A':
				code.WriteRune('X')
			case next == 'H':
				if prev == 'S' {
					code.WriteRune('K')
				} else {
					code.WriteRune('X')
				}
			case next == 'I' || next == 'E' || next == 'Y':
				if prev != 'S' {
					code.WriteRune('S')
				}
			default:
				code.WriteRune('K')
			}
		case 'D':
			if next == 'G' && (next2 == 'E' || next2 == 'Y' || next2 == 'I') {
				code.WriteRune('J')
			} else {
				code.WriteRune('T')
			}
		case 'G':
			switch {
			case next == 'H' && i+1 == n-1:
			case next == 'N':
			case next == 'I' || next == 'E' || next == 'Y':
				code.WriteRune('J')
			default:
				code.WriteRune('K')
			}
		case 'H':
			if isVowel(prev) && !isVowel(next) {
				// silent
			} else if prev == 'C' || prev == 'S' || prev == 'P' || prev == 'T' || prev == 'G' {
				// silent, consumed by the preceding digraph rule
			} else {
				code.WriteRune('H')
			}
		case 'K':
			if prev != 'C' {
				code.WriteRune('K')
			}
		case 'P':
			if next == 'H' {
				code.WriteRune('F')
			} else {
				code.WriteRune('P')
			}
		case 'Q':
			code.WriteRune('K')
		case 'S':
			switch {
			case next == 'H':
				code.WriteRune('X')
			case next == 'I' && (next2 == 'O' || next2 == 'A'):
				code.WriteRune('X')
			default:
				code.WriteRune('S')
			}
		case 'T':
			switch {
			case next == 'I' && (next2 == 'O' || next2 == 'A'):
				code.WriteRune('X')
			case next == 'H':
				code.WriteRune('0')
			default:
				code.WriteRune('T')
			}
		case 'V':
			code.WriteRune('F')
		case 'W', 'Y':
			if isVowel(next) {
				code.WriteRune(c)
			}
		case 'X':
			code.WriteString("KS")
		case 'Z':
			code.WriteRune('S')
		case 'F', 'J', 'L', 'M', 'N', 'R':
			code.WriteRune(c)
		}

		last = c
	}

	return code.String()
}

// Compress compresses data using the specified algorithm (gzip, zlib or
// flate) and returns the result base64-encoded.
func Compress(data, algorithm string) (string, error) {
	var buf bytes.Buffer
	var w io.WriteCloser
	var err error

	switch strings.ToLower(algorithm) {
	case "gzip":
		w = gzip.NewWriter(&buf)
	case "zlib":
		w = zlib.NewWriter(&buf)
	case "flate", "deflate":
		w, err = flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	if _, err := w.Write([]byte(data)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// Decompress reverses Compress for the specified algorithm.
func Decompress(data, algorithm string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	var r io.ReadCloser
	switch strings.ToLower(algorithm) {
	case "gzip":
		r, err = gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
	case "zlib":
		r, err = zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
	case "flate", "deflate":
		r = flate.NewReader(bytes.NewReader(raw))
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// RegexMatch returns all matches of pattern within text.
func RegexMatch(pattern, text string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.FindAllString(text, -1), nil
}

// RegexReplace replaces all matches of pattern within text with replacement.
func RegexReplace(pattern, text, replacement string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(text, replacement), nil
}

// RegexExplain produces a human-readable, token-by-token explanation of a
// regular expression.
func RegexExplain(pattern string) string {
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Sprintf("invalid pattern: %v", err)
	}

	runes := []rune(pattern)
	var sb strings.Builder
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '^':
			sb.WriteString("^ start of string/line\n")
		case '$':
			sb.WriteString("$ end of string/line\n")
		case '.':
			sb.WriteString(". any character except newline\n")
		case '*':
			sb.WriteString("* zero or more of preceding token\n")
		case '+':
			sb.WriteString("+ one or more of preceding token\n")
		case '?':
			sb.WriteString("? zero or one of preceding token\n")
		case '|':
			sb.WriteString("| alternation (or)\n")
		case '\\':
			if i+1 < len(runes) {
				i++
				sb.WriteString(fmt.Sprintf("\\%c escaped or special character class\n", runes[i]))
			}
		case '(':
			sb.WriteString("( start of group\n")
		case ')':
			sb.WriteString(") end of group\n")
		case '[':
			sb.WriteString("[ start of character class\n")
		case ']':
			sb.WriteString("] end of character class\n")
		case '{':
			sb.WriteString("{ start of repetition count\n")
		case '}':
			sb.WriteString("} end of repetition count\n")
		default:
			sb.WriteString(fmt.Sprintf("%c literal character\n", c))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

var (
	mdHeaderRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	mdBoldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	mdItalicRe = regexp.MustCompile(`\*(.+?)\*`)
	mdCodeRe   = regexp.MustCompile("`([^`]+)`")
	mdLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	mdQuoteRe  = regexp.MustCompile(`(?m)^>\s?`)
	mdListRe   = regexp.MustCompile(`(?m)^[-*+]\s+`)
)

// MarkdownToHTML converts a common subset of Markdown (headers, bold,
// italic, inline code, links and unordered lists) to HTML.
func MarkdownToHTML(md string) string {
	out := md

	out = mdHeaderRe.ReplaceAllStringFunc(out, func(m string) string {
		parts := mdHeaderRe.FindStringSubmatch(m)
		level := len(parts[1])
		return fmt.Sprintf("<h%d>%s</h%d>", level, parts[2], level)
	})
	out = mdBoldRe.ReplaceAllString(out, "<strong>$1</strong>")
	out = mdItalicRe.ReplaceAllString(out, "<em>$1</em>")
	out = mdCodeRe.ReplaceAllString(out, "<code>$1</code>")
	out = mdLinkRe.ReplaceAllString(out, `<a href="$2">$1</a>`)

	lines := strings.Split(out, "\n")
	var result []string
	inList := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			if !inList {
				result = append(result, "<ul>")
				inList = true
			}
			result = append(result, "<li>"+strings.TrimPrefix(trimmed, "- ")+"</li>")
			continue
		}
		if inList {
			result = append(result, "</ul>")
			inList = false
		}
		result = append(result, line)
	}
	if inList {
		result = append(result, "</ul>")
	}

	return strings.Join(result, "\n")
}

// MarkdownTOC builds a nested table-of-contents list from Markdown headers.
func MarkdownTOC(md string) string {
	matches := mdHeaderRe.FindAllStringSubmatch(md, -1)
	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, m := range matches {
		level := len(m[1])
		title := strings.TrimSpace(m[2])
		anchor := Slugify(title)
		indent := strings.Repeat("  ", level-1)
		sb.WriteString(fmt.Sprintf("%s- [%s](#%s)\n", indent, title, anchor))
	}

	return strings.TrimRight(sb.String(), "\n")
}

var bbcodeReplacements = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?is)\[b\](.*?)\[/b\]`), "<strong>$1</strong>"},
	{regexp.MustCompile(`(?is)\[i\](.*?)\[/i\]`), "<em>$1</em>"},
	{regexp.MustCompile(`(?is)\[u\](.*?)\[/u\]`), "<u>$1</u>"},
	{regexp.MustCompile(`(?is)\[s\](.*?)\[/s\]`), "<s>$1</s>"},
	{regexp.MustCompile(`(?is)\[url=([^\]]+)\](.*?)\[/url\]`), `<a href="$1">$2</a>`},
	{regexp.MustCompile(`(?is)\[url\](.*?)\[/url\]`), `<a href="$1">$1</a>`},
	{regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`), `<img src="$1">`},
	{regexp.MustCompile(`(?is)\[quote\](.*?)\[/quote\]`), "<blockquote>$1</blockquote>"},
	{regexp.MustCompile(`(?is)\[code\](.*?)\[/code\]`), "<pre><code>$1</code></pre>"},
	{regexp.MustCompile(`(?is)\[list\](.*?)\[/list\]`), "<ul>$1</ul>"},
	{regexp.MustCompile(`(?is)\[\*\]\s*(.*?)\n`), "<li>$1</li>\n"},
}

// BBCodeToHTML converts common BBCode tags to their HTML equivalents.
func BBCodeToHTML(bb string) string {
	result := bb
	for _, r := range bbcodeReplacements {
		result = r.re.ReplaceAllString(result, r.repl)
	}
	return result
}

var htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// HTMLToText strips HTML tags and decodes entities, leaving plain text.
func HTMLToText(input string) string {
	stripped := htmlTagRe.ReplaceAllString(input, "")
	return html.UnescapeString(strings.TrimSpace(stripped))
}

// ROT47 applies the ROT47 cipher, rotating printable ASCII characters.
func ROT47(input string) string {
	return strings.Map(func(r rune) rune {
		if r >= '!' && r <= '~' {
			return '!' + (r-'!'+47)%94
		}
		return r
	}, input)
}

// Caesar applies a Caesar shift cipher to alphabetic characters.
func Caesar(input string, shift int) string {
	shift = ((shift % 26) + 26) % 26
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return 'a' + (r-'a'+rune(shift))%26
		case r >= 'A' && r <= 'Z':
			return 'A' + (r-'A'+rune(shift))%26
		}
		return r
	}, input)
}

// Vigenere applies the Vigenere polyalphabetic cipher using key.
func Vigenere(input, key string) string {
	var keyRunes []rune
	for _, r := range strings.ToUpper(key) {
		if unicode.IsLetter(r) {
			keyRunes = append(keyRunes, r)
		}
	}
	if len(keyRunes) == 0 {
		return input
	}

	ki := 0
	out := make([]rune, 0, len(input))
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			shift := keyRunes[ki%len(keyRunes)] - 'A'
			out = append(out, 'a'+(r-'a'+shift)%26)
			ki++
		case r >= 'A' && r <= 'Z':
			shift := keyRunes[ki%len(keyRunes)] - 'A'
			out = append(out, 'A'+(r-'A'+shift)%26)
			ki++
		default:
			out = append(out, r)
		}
	}

	return string(out)
}

// Binary converts input to a space-separated string of 8-bit binary bytes.
func Binary(input string) string {
	bs := []byte(input)
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = fmt.Sprintf("%08b", b)
	}
	return strings.Join(parts, " ")
}

var morseTable = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.",
	'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
}

// Morse converts input text to International Morse code, with letters
// separated by spaces and words separated by "/".
func Morse(input string) string {
	words := strings.Fields(input)
	out := make([]string, 0, len(words))
	for _, word := range words {
		var letters []string
		for _, r := range strings.ToUpper(word) {
			if code, ok := morseTable[r]; ok {
				letters = append(letters, code)
			}
		}
		out = append(out, strings.Join(letters, " "))
	}
	return strings.Join(out, " / ")
}

var (
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	urlRe   = regexp.MustCompile(`https?://[^\s<>"']+`)
	ipRe    = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)
	phoneRe = regexp.MustCompile(`\+?\d{1,3}[-.\s]?\(?\d{2,4}\)?[-.\s]?\d{3,4}[-.\s]?\d{3,4}`)
)

// ExtractEmails returns all email addresses found in text.
func ExtractEmails(text string) []string {
	matches := emailRe.FindAllString(text, -1)
	if matches == nil {
		return []string{}
	}
	return matches
}

// ExtractURLs returns all http(s) URLs found in text.
func ExtractURLs(text string) []string {
	matches := urlRe.FindAllString(text, -1)
	if matches == nil {
		return []string{}
	}
	return matches
}

// ExtractIPs returns all valid IPv4 addresses found in text.
func ExtractIPs(text string) []string {
	candidates := ipRe.FindAllString(text, -1)
	valid := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if net.ParseIP(c) != nil {
			valid = append(valid, c)
		}
	}
	return valid
}

// ExtractPhones returns candidate phone numbers found in text.
func ExtractPhones(text string) []string {
	matches := phoneRe.FindAllString(text, -1)
	if matches == nil {
		return []string{}
	}
	return matches
}

// Lines splits text into lines.
func Lines(text string) []string {
	return strings.Split(text, "\n")
}

// Dedupe removes duplicate lines while preserving first-seen order.
func Dedupe(lines []string) []string {
	seen := make(map[string]bool, len(lines))
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			result = append(result, l)
		}
	}
	return result
}

// Sort returns a new slice with lines sorted lexicographically.
func Sort(lines []string) []string {
	result := make([]string, len(lines))
	copy(result, lines)
	sort.Strings(result)
	return result
}

// Shuffle returns a new slice with lines in random order.
func Shuffle(lines []string) []string {
	result := make([]string, len(lines))
	copy(result, lines)
	rng.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})
	return result
}

// Trim removes leading and trailing whitespace.
func Trim(input string) string {
	return strings.TrimSpace(input)
}

// StripHTML removes HTML tags from input.
func StripHTML(input string) string {
	return strings.TrimSpace(htmlTagRe.ReplaceAllString(input, ""))
}

var (
	stripMdHeaderRe = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	stripMdImageRe  = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	stripMdLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
)

// StripMarkdown removes common Markdown syntax markers, leaving plain text.
func StripMarkdown(input string) string {
	result := input
	result = stripMdHeaderRe.ReplaceAllString(result, "")
	result = mdBoldRe.ReplaceAllString(result, "$1")
	result = mdItalicRe.ReplaceAllString(result, "$1")
	result = mdCodeRe.ReplaceAllString(result, "$1")
	result = stripMdImageRe.ReplaceAllString(result, "$1")
	result = stripMdLinkRe.ReplaceAllString(result, "$1")
	result = mdQuoteRe.ReplaceAllString(result, "")
	result = mdListRe.ReplaceAllString(result, "")
	return result
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// StripANSI removes ANSI escape sequences from input.
func StripANSI(input string) string {
	return ansiRe.ReplaceAllString(input, "")
}

var hipsterWords = []string{
	"artisan", "kombucha", "biodiesel", "tote bag", "gastropub", "vinyl",
	"cold-pressed", "organic", "kale chips", "sriracha", "meggings", "synth",
	"tousled", "distillery", "authentic", "forage", "hashtag", "letterpress",
	"normcore", "umami", "chillwave", "microdosing", "fixie", "succulents",
	"tattooed", "farm-to-table", "cronut", "raclette", "paleo", "waistcoat",
}

var baconWords = []string{
	"bacon", "pork belly", "ham hock", "sausage", "brisket", "pastrami",
	"short loin", "tenderloin", "picanha", "chuck", "meatball", "turducken",
	"ribeye", "porchetta", "salami", "corned beef", "kielbasa", "flank",
	"pancetta", "tri-tip", "jerky", "cupim", "landjaeger", "spare ribs",
	"filet mignon", "boudin", "shank", "t-bone",
}

var cupcakeWords = []string{
	"cupcake", "icing", "sugar plum", "chocolate bar", "candy", "donut",
	"marshmallow", "jelly bean", "gummies", "sweet roll", "cotton candy",
	"caramel", "toffee", "macaroon", "brownie", "danish", "tart", "wafer",
	"cheesecake", "biscuit", "pastry", "muffin", "fruitcake", "sprinkles",
	"gingerbread",
}

var pirateWords = []string{
	"ahoy", "matey", "scallywag", "landlubber", "booty", "doubloons", "grog",
	"plank", "cutlass", "parrot", "treasure", "galleon", "cannon",
	"buccaneer", "seven seas", "kraken", "rum", "black flag", "first mate",
	"anchor", "harbor", "corsair", "pillage",
}

var zombieWords = []string{
	"brains", "undead", "graveyard", "rotting", "shamble", "infection",
	"apocalypse", "virus", "moan", "decay", "crypt", "outbreak", "survivor",
	"horde", "flesh", "reanimated", "plague", "quarantine", "cadaver",
	"ghoul", "corpse", "bite", "contagion",
}

var corporateWords = []string{
	"synergy", "leverage", "paradigm", "bandwidth", "deliverable",
	"stakeholder", "actionable", "holistic", "disrupt", "scalable",
	"proactive", "streamline", "incentivize", "optimize", "robust",
	"ecosystem", "pivot", "onboard", "touchpoint", "alignment",
	"best practice", "value-add", "deep dive",
}

var techWords = []string{
	"blockchain", "cloud-native", "microservice", "serverless", "kubernetes",
	"container", "API", "machine learning", "neural network", "devops",
	"edge computing", "quantum", "cybersecurity", "open-source", "refactor",
	"latency", "throughput", "scalability", "encryption", "distributed",
	"asynchronous", "framework", "repository", "pipeline",
}

// themedSentences builds count sentences of random length from words.
func themedSentences(words []string, count int) string {
	if count <= 0 {
		count = 1
	}
	sentences := make([]string, count)
	for i := 0; i < count; i++ {
		wc := 6 + rng.Intn(8)
		w := make([]string, wc)
		for j := range w {
			w[j] = words[rng.Intn(len(words))]
		}
		w[0] = strings.ToUpper(w[0][:1]) + w[0][1:]
		sentences[i] = strings.Join(w, " ") + "."
	}
	return strings.Join(sentences, " ")
}

// themedText generates themed placeholder text as words, sentences or
// paragraphs depending on typ.
func themedText(words []string, count int, typ string) string {
	if count <= 0 {
		count = 5
	}

	switch strings.ToLower(typ) {
	case "word", "words":
		out := make([]string, count)
		for i := range out {
			out[i] = words[rng.Intn(len(words))]
		}
		return strings.Join(out, " ")
	case "paragraph", "paragraphs":
		paras := make([]string, count)
		for i := range paras {
			paras[i] = themedSentences(words, 3+rng.Intn(3))
		}
		return strings.Join(paras, "\n\n")
	default:
		return themedSentences(words, count)
	}
}

func Hipsum(count int, typ string) string    { return themedText(hipsterWords, count, typ) }
func Bacon(count int, typ string) string     { return themedText(baconWords, count, typ) }
func Cupcake(count int, typ string) string   { return themedText(cupcakeWords, count, typ) }
func Pirate(count int, typ string) string    { return themedText(pirateWords, count, typ) }
func Zombie(count int, typ string) string    { return themedText(zombieWords, count, typ) }
func Corporate(count int, typ string) string { return themedText(corporateWords, count, typ) }
func TechIpsum(count int, typ string) string { return themedText(techWords, count, typ) }

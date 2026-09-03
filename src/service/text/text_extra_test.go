package text

import (
	"strings"
	"testing"
)

func TestUUID(t *testing.T) {
	cases := []int{1, 4, 7, 0, 99}
	for _, v := range cases {
		id, err := UUID(v)
		if err != nil {
			t.Fatalf("UUID(%d) error: %v", v, err)
		}
		if len(id) != 36 {
			t.Errorf("UUID(%d) = %q, want length 36", v, id)
		}
	}
}

func TestUUIDs(t *testing.T) {
	ids, err := UUIDs(4, 5)
	if err != nil {
		t.Fatalf("UUIDs error: %v", err)
	}
	if len(ids) != 5 {
		t.Errorf("UUIDs(4, 5) returned %d ids, want 5", len(ids))
	}

	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("UUIDs() produced duplicate: %s", id)
		}
		seen[id] = true
	}

	// count <= 0 clamps to 1
	ids, err = UUIDs(4, 0)
	if err != nil {
		t.Fatalf("UUIDs(4, 0) error: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("UUIDs(4, 0) = %d ids, want 1", len(ids))
	}

	// count > 1000 clamps to 1000
	ids, err = UUIDs(4, 5000)
	if err != nil {
		t.Fatalf("UUIDs(4, 5000) error: %v", err)
	}
	if len(ids) != 1000 {
		t.Errorf("UUIDs(4, 5000) = %d ids, want 1000", len(ids))
	}
}

func TestHash(t *testing.T) {
	cases := []struct {
		algo string
		want string
	}{
		{"md5", "5eb63bbbe01eeed093cb22bb8f5acdc3"},
		{"sha1", "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"},
		{"sha256", "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"},
		{"SHA256", "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"},
	}
	for _, c := range cases {
		got, err := Hash(c.algo, "hello world")
		if err != nil {
			t.Fatalf("Hash(%s) error: %v", c.algo, err)
		}
		if got != c.want {
			t.Errorf("Hash(%s) = %q, want %q", c.algo, got, c.want)
		}
	}

	// sha384 and sha512 should succeed and produce non-empty hex output
	for _, algo := range []string{"sha384", "sha512"} {
		got, err := Hash(algo, "hello world")
		if err != nil {
			t.Fatalf("Hash(%s) error: %v", algo, err)
		}
		if got == "" {
			t.Errorf("Hash(%s) returned empty string", algo)
		}
	}

	if _, err := Hash("bogus", "x"); err == nil {
		t.Errorf("Hash(bogus) should return an error")
	}
}

func TestHashAll(t *testing.T) {
	hashes := HashAll("hello")
	for _, alg := range []string{"md5", "sha1", "sha256", "sha384", "sha512"} {
		if hashes[alg] == "" {
			t.Errorf("HashAll()[%s] is empty", alg)
		}
	}
	if len(hashes) != 5 {
		t.Errorf("HashAll() returned %d entries, want 5", len(hashes))
	}
}

func TestBase64(t *testing.T) {
	enc := Base64Encode("hello world")
	if enc != "aGVsbG8gd29ybGQ=" {
		t.Errorf("Base64Encode() = %q", enc)
	}
	dec, err := Base64Decode(enc)
	if err != nil || dec != "hello world" {
		t.Errorf("Base64Decode() = %q, %v", dec, err)
	}
	if _, err := Base64Decode("not valid base64!!"); err == nil {
		t.Errorf("Base64Decode() with invalid input should error")
	}

	uenc := Base64URLEncode("hello world")
	udec, err := Base64URLDecode(uenc)
	if err != nil || udec != "hello world" {
		t.Errorf("Base64URLDecode() = %q, %v", udec, err)
	}
	if _, err := Base64URLDecode("not valid!!"); err == nil {
		t.Errorf("Base64URLDecode() with invalid input should error")
	}
}

func TestBase32(t *testing.T) {
	enc := Base32Encode("hello")
	dec, err := Base32Decode(enc)
	if err != nil || dec != "hello" {
		t.Errorf("Base32Decode(Base32Encode(hello)) = %q, %v", dec, err)
	}
	if _, err := Base32Decode("not-valid-base32!!!"); err == nil {
		t.Errorf("Base32Decode() with invalid input should error")
	}
}

func TestHexEncoding(t *testing.T) {
	enc := HexEncode("hi")
	if enc != "6869" {
		t.Errorf("HexEncode(hi) = %q, want 6869", enc)
	}
	dec, err := HexDecode(enc)
	if err != nil || dec != "hi" {
		t.Errorf("HexDecode() = %q, %v", dec, err)
	}
	if _, err := HexDecode("zz"); err == nil {
		t.Errorf("HexDecode() with invalid input should error")
	}
}

func TestURLEncoding(t *testing.T) {
	enc := URLEncode("a b&c")
	if enc != "a+b%26c" {
		t.Errorf("URLEncode() = %q, want a+b%%26c", enc)
	}
	dec, err := URLDecode(enc)
	if err != nil || dec != "a b&c" {
		t.Errorf("URLDecode() = %q, %v", dec, err)
	}
	if _, err := URLDecode("%zz"); err == nil {
		t.Errorf("URLDecode() with invalid escape should error")
	}
}

func TestCaseConversions(t *testing.T) {
	if got := ToLower("HeLLo"); got != "hello" {
		t.Errorf("ToLower() = %q", got)
	}
	if got := ToUpper("HeLLo"); got != "HELLO" {
		t.Errorf("ToUpper() = %q", got)
	}
	if got := ToTitle("hello world"); got != "Hello World" {
		t.Errorf("ToTitle() = %q", got)
	}
	if got := ToCamelCase("hello world foo"); got != "helloWorldFoo" {
		t.Errorf("ToCamelCase() = %q", got)
	}
	if got := ToCamelCase(""); got != "" {
		t.Errorf("ToCamelCase(empty) = %q, want empty", got)
	}
	if got := ToSnakeCase("Hello World"); got != "hello_world" {
		t.Errorf("ToSnakeCase() = %q", got)
	}
	if got := ToKebabCase("Hello World"); got != "hello-world" {
		t.Errorf("ToKebabCase() = %q", got)
	}
	if got := ToScreamingSnake("hello world"); got != "HELLO_WORLD" {
		t.Errorf("ToScreamingSnake() = %q", got)
	}
	if got := ToDotCase("hello world"); got != "hello.world" {
		t.Errorf("ToDotCase() = %q", got)
	}
}

func TestReverse(t *testing.T) {
	if got := Reverse("hello"); got != "olleh" {
		t.Errorf("Reverse(hello) = %q, want olleh", got)
	}
	if got := Reverse(""); got != "" {
		t.Errorf("Reverse(empty) = %q, want empty", got)
	}
}

func TestStats(t *testing.T) {
	s := Stats("hello world\nfoo")
	if s["characters"] != 15 {
		t.Errorf("Stats()[characters] = %v, want 15", s["characters"])
	}
	if s["words"] != 3 {
		t.Errorf("Stats()[words] = %v, want 3", s["words"])
	}
	if s["lines"] != 2 {
		t.Errorf("Stats()[lines] = %v, want 2", s["lines"])
	}
}

func TestCount(t *testing.T) {
	c := Count("hello world\nfoo")
	if c["chars"] != 15 {
		t.Errorf("Count()[chars] = %d, want 15", c["chars"])
	}
	if c["words"] != 3 {
		t.Errorf("Count()[words] = %d, want 3", c["words"])
	}
	if c["lines"] != 2 {
		t.Errorf("Count()[lines] = %d, want 2", c["lines"])
	}
}

func TestROT13(t *testing.T) {
	if got := ROT13("Hello"); got != "Uryyb" {
		t.Errorf("ROT13(Hello) = %q, want Uryyb", got)
	}
	if got := ROT13(ROT13("Hello")); got != "Hello" {
		t.Errorf("ROT13(ROT13(x)) = %q, want Hello", got)
	}
}

func TestTrim(t *testing.T) {
	if got := Trim("  hello  "); got != "hello" {
		t.Errorf("Trim() = %q, want hello", got)
	}
}

func TestLorem(t *testing.T) {
	if words := LoremWords(5); len(words) != 5 {
		t.Errorf("LoremWords(5) = %d words, want 5", len(words))
	}
	if words := LoremWords(0); len(words) != 10 {
		t.Errorf("LoremWords(0) = %d words, want default 10", len(words))
	}
	if words := LoremWords(5000); len(words) != 1000 {
		t.Errorf("LoremWords(5000) = %d words, want clamped 1000", len(words))
	}

	if s := LoremSentences(3); len(s) != 3 {
		t.Errorf("LoremSentences(3) = %d sentences, want 3", len(s))
	}
	if s := LoremSentences(0); len(s) != 5 {
		t.Errorf("LoremSentences(0) = %d sentences, want default 5", len(s))
	}
	if s := LoremSentences(500); len(s) != 100 {
		t.Errorf("LoremSentences(500) = %d sentences, want clamped 100", len(s))
	}
	for _, sentence := range LoremSentences(1) {
		if !strings.HasSuffix(sentence, ".") {
			t.Errorf("LoremSentences() sentence %q missing trailing period", sentence)
		}
	}

	if p := LoremParagraphs(2); len(p) != 2 {
		t.Errorf("LoremParagraphs(2) = %d paragraphs, want 2", len(p))
	}
	if p := LoremParagraphs(0); len(p) != 3 {
		t.Errorf("LoremParagraphs(0) = %d paragraphs, want default 3", len(p))
	}
	if p := LoremParagraphs(50); len(p) != 20 {
		t.Errorf("LoremParagraphs(50) = %d paragraphs, want clamped 20", len(p))
	}
}

func TestRegexExplain(t *testing.T) {
	out := RegexExplain(`^a.*$`)
	for _, want := range []string{"^ start", ". any character", "* zero or more", "$ end"} {
		if !strings.Contains(out, want) {
			t.Errorf("RegexExplain() missing %q in %q", want, out)
		}
	}

	out = RegexExplain(`(`)
	if !strings.HasPrefix(out, "invalid pattern:") {
		t.Errorf("RegexExplain(invalid) = %q, want invalid pattern prefix", out)
	}
}

func TestMarkdownTOC(t *testing.T) {
	toc := MarkdownTOC("# Title\n## Sub Section\ntext")
	if !strings.Contains(toc, "- [Title](#title)") {
		t.Errorf("MarkdownTOC() missing top-level entry: %q", toc)
	}
	if !strings.Contains(toc, "  - [Sub Section](#sub-section)") {
		t.Errorf("MarkdownTOC() missing indented sub entry: %q", toc)
	}

	if got := MarkdownTOC("no headers here"); got != "" {
		t.Errorf("MarkdownTOC(no headers) = %q, want empty", got)
	}
}

func TestExtractPhones(t *testing.T) {
	phones := ExtractPhones("Call +1-555-123-4567 or 555.987.6543 today")
	if len(phones) != 2 {
		t.Errorf("ExtractPhones() = %v, want 2 matches", phones)
	}

	if got := ExtractPhones("no phone numbers"); len(got) != 0 {
		t.Errorf("ExtractPhones(no match) = %v, want empty", got)
	}
}

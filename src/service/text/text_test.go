package text

import (
	"strings"
	"testing"
)

func TestIDGenerators(t *testing.T) {
	if id := ULID(); len(id) != 26 {
		t.Errorf("ULID() length = %d, want 26 (id=%q)", len(id), id)
	}
	if id := NanoID(); len(id) != 21 {
		t.Errorf("NanoID() length = %d, want 21 (id=%q)", len(id), id)
	}
	if id := KSUID(); len(id) != 27 {
		t.Errorf("KSUID() length = %d, want 27 (id=%q)", len(id), id)
	}
	if id := XID(); len(id) != 20 {
		t.Errorf("XID() length = %d, want 20 (id=%q)", len(id), id)
	}
	if id := CUID(); !strings.HasPrefix(id, "c") || len(id) != 25 {
		t.Errorf("CUID() = %q, want prefix c and length 25", id)
	}
	if id := Snowflake(); id == "" {
		t.Errorf("Snowflake() returned empty string")
	}
	if id := ObjectID(); len(id) != 24 {
		t.Errorf("ObjectID() length = %d, want 24 (id=%q)", len(id), id)
	}

	// Uniqueness across successive calls
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		id := XID()
		if seen[id] {
			t.Errorf("XID() produced duplicate: %s", id)
		}
		seen[id] = true
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello World", "hello-world"},
		{"  Multiple   Spaces  ", "multiple-spaces"},
		{"Special!!Chars??Here", "special-chars-here"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToPascalCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello world", "HelloWorld"},
		{"THE QUICK FOX", "TheQuickFox"},
	}
	for _, c := range cases {
		if got := ToPascalCase(c.in); got != c.want {
			t.Errorf("ToPascalCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDiff(t *testing.T) {
	out := Diff("a\nb\nc", "a\nx\nc")
	if !strings.Contains(out, "  a") || !strings.Contains(out, "- b") ||
		!strings.Contains(out, "+ x") || !strings.Contains(out, "  c") {
		t.Errorf("Diff() = %q, missing expected line markers", out)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		s1, s2 string
		want   int
	}{
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"same", "same", 0},
	}
	for _, c := range cases {
		if got := Levenshtein(c.s1, c.s2); got != c.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", c.s1, c.s2, got, c.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	if got := Similarity("same", "same"); got != 1.0 {
		t.Errorf("Similarity(same, same) = %v, want 1.0", got)
	}
	if got := Similarity("", ""); got != 1.0 {
		t.Errorf("Similarity(\"\", \"\") = %v, want 1.0", got)
	}
	if got := Similarity("abc", "xyz"); got != 0.0 {
		t.Errorf("Similarity(abc, xyz) = %v, want 0.0", got)
	}
}

func TestSoundex(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Robert", "R163"},
		{"Rupert", "R163"},
		{"Ashcraft", "A261"},
	}
	for _, c := range cases {
		if got := Soundex(c.in); got != c.want {
			t.Errorf("Soundex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMetaphone(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Smith", "SM0"},
		{"Knight", "NKT"},
		{"Phone", "FN"},
	}
	for _, c := range cases {
		if got := Metaphone(c.in); got != c.want {
			t.Errorf("Metaphone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompressDecompress(t *testing.T) {
	for _, algo := range []string{"gzip", "zlib", "flate"} {
		original := "the quick brown fox jumps over the lazy dog"
		compressed, err := Compress(original, algo)
		if err != nil {
			t.Fatalf("Compress(%s) error: %v", algo, err)
		}
		decompressed, err := Decompress(compressed, algo)
		if err != nil {
			t.Fatalf("Decompress(%s) error: %v", algo, err)
		}
		if decompressed != original {
			t.Errorf("Decompress(Compress(%s)) = %q, want %q", algo, decompressed, original)
		}
	}
}

func TestRegexMatch(t *testing.T) {
	matches, err := RegexMatch(`\d+`, "abc123def456")
	if err != nil {
		t.Fatalf("RegexMatch error: %v", err)
	}
	if len(matches) != 2 || matches[0] != "123" || matches[1] != "456" {
		t.Errorf("RegexMatch() = %v, want [123 456]", matches)
	}

	if _, err := RegexMatch("(", "text"); err == nil {
		t.Errorf("RegexMatch with invalid pattern should error")
	}
}

func TestRegexReplace(t *testing.T) {
	out, err := RegexReplace(`\d+`, "abc123def456", "#")
	if err != nil {
		t.Fatalf("RegexReplace error: %v", err)
	}
	if out != "abc#def#" {
		t.Errorf("RegexReplace() = %q, want %q", out, "abc#def#")
	}
}

func TestMarkdownToHTML(t *testing.T) {
	out := MarkdownToHTML("# Title\n**bold** and *italic*")
	if !strings.Contains(out, "<h1>Title</h1>") {
		t.Errorf("MarkdownToHTML() missing h1: %q", out)
	}
	if !strings.Contains(out, "<strong>bold</strong>") {
		t.Errorf("MarkdownToHTML() missing strong: %q", out)
	}
	if !strings.Contains(out, "<em>italic</em>") {
		t.Errorf("MarkdownToHTML() missing em: %q", out)
	}
}

func TestBBCodeToHTML(t *testing.T) {
	out := BBCodeToHTML("[b]bold[/b] [i]italic[/i]")
	if !strings.Contains(out, "<strong>bold</strong>") || !strings.Contains(out, "<em>italic</em>") {
		t.Errorf("BBCodeToHTML() = %q", out)
	}
}

func TestHTMLToText(t *testing.T) {
	out := HTMLToText("<p>Hello &amp; <b>World</b></p>")
	if out != "Hello & World" {
		t.Errorf("HTMLToText() = %q, want %q", out, "Hello & World")
	}
}

func TestCiphers(t *testing.T) {
	if got := ROT47("Hello"); got == "Hello" {
		t.Errorf("ROT47() did not transform input")
	}
	if got := ROT47(ROT47("Hello")); got != "Hello" {
		t.Errorf("ROT47(ROT47(x)) = %q, want %q", got, "Hello")
	}

	if got := Caesar("abc", 1); got != "bcd" {
		t.Errorf("Caesar(abc, 1) = %q, want bcd", got)
	}
	if got := Caesar("bcd", -1); got != "abc" {
		t.Errorf("Caesar(bcd, -1) = %q, want abc", got)
	}

	enc := Vigenere("ATTACKATDAWN", "LEMON")
	if enc != "LXFOPVEFRNHR" {
		t.Errorf("Vigenere() = %q, want LXFOPVEFRNHR", enc)
	}
}

func TestBinary(t *testing.T) {
	if got := Binary("A"); got != "01000001" {
		t.Errorf("Binary(A) = %q, want 01000001", got)
	}
}

func TestMorse(t *testing.T) {
	if got := Morse("SOS"); got != "... --- ..." {
		t.Errorf("Morse(SOS) = %q, want ... --- ...", got)
	}
}

func TestExtraction(t *testing.T) {
	text := "Contact us at foo@example.com or visit https://example.com. Server at 192.168.1.1."

	emails := ExtractEmails(text)
	if len(emails) != 1 || emails[0] != "foo@example.com" {
		t.Errorf("ExtractEmails() = %v", emails)
	}

	urls := ExtractURLs(text)
	if len(urls) != 1 {
		t.Errorf("ExtractURLs() = %v", urls)
	}

	ips := ExtractIPs(text)
	if len(ips) != 1 || ips[0] != "192.168.1.1" {
		t.Errorf("ExtractIPs() = %v", ips)
	}

	if got := ExtractEmails("no emails here"); len(got) != 0 {
		t.Errorf("ExtractEmails() on no-match text = %v, want empty", got)
	}
}

func TestLineOperations(t *testing.T) {
	lines := Lines("b\na\nb\nc")
	if len(lines) != 4 {
		t.Errorf("Lines() = %v, want 4 elements", lines)
	}

	deduped := Dedupe(lines)
	if len(deduped) != 3 {
		t.Errorf("Dedupe() = %v, want 3 unique elements", deduped)
	}

	sorted := Sort([]string{"c", "a", "b"})
	if strings.Join(sorted, "") != "abc" {
		t.Errorf("Sort() = %v, want [a b c]", sorted)
	}

	original := []string{"1", "2", "3", "4", "5"}
	shuffled := Shuffle(original)
	if len(shuffled) != len(original) {
		t.Errorf("Shuffle() changed length")
	}
	if original[0] != "1" {
		t.Errorf("Shuffle() mutated the original slice")
	}
}

func TestStripping(t *testing.T) {
	if got := StripHTML("<p>Hello <b>World</b></p>"); got != "Hello World" {
		t.Errorf("StripHTML() = %q, want %q", got, "Hello World")
	}

	if got := StripMarkdown("# Title\n**bold** [link](http://x.com)"); strings.Contains(got, "#") || strings.Contains(got, "*") {
		t.Errorf("StripMarkdown() = %q, markdown syntax remains", got)
	}

	if got := StripANSI("\x1b[31mred\x1b[0m"); got != "red" {
		t.Errorf("StripANSI() = %q, want %q", got, "red")
	}
}

func TestThemedLorem(t *testing.T) {
	generators := map[string]func(int, string) string{
		"Hipsum":    Hipsum,
		"Bacon":     Bacon,
		"Cupcake":   Cupcake,
		"Pirate":    Pirate,
		"Zombie":    Zombie,
		"Corporate": Corporate,
		"TechIpsum": TechIpsum,
	}

	for name, fn := range generators {
		if out := fn(5, "words"); out == "" {
			t.Errorf("%s(5, words) produced empty output", name)
		}
		if out := fn(2, "sentences"); !strings.Contains(out, ".") {
			t.Errorf("%s(2, sentences) = %q, missing sentence terminator", name, out)
		}
		if out := fn(2, "paragraphs"); !strings.Contains(out, "\n\n") {
			t.Errorf("%s(2, paragraphs) = %q, missing paragraph break", name, out)
		}
	}
}

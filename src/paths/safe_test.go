package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
		err   error
	}{
		{input: "/my//admin/", want: "my/admin"},
		{input: "", want: ""},
		{input: "/../admin", err: ErrPathTraversal},
		{input: "/Admin", err: ErrInvalidPath},
		{input: "/", want: ""},
	}
	for _, test := range tests {
		got, err := SafePath(test.input)
		if !errors.Is(err, test.err) || got != test.want {
			t.Errorf("SafePath(%q) = %q, %v; want %q, %v", test.input, got, err, test.want, test.err)
		}
	}
}

func TestSafeResourcePathPreservesFileNames(t *testing.T) {
	for input, want := range map[string]string{
		"/static/css/main.css": "static/css/main.css",
		"/robots.txt":          "robots.txt",
		"Report_2024.PDF":      "Report_2024.PDF",
		"//assets//logo.svg//": "assets/logo.svg",
	} {
		got, err := SafeResourcePath(input)
		if err != nil || got != want {
			t.Errorf("SafeResourcePath(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestSafeResourcePathRejectsUnsafeNames(t *testing.T) {
	for _, input := range []string{"", ".secret", "../secret", "reports..old/file", `assets\\logo.svg`, "bad name.txt", strings.Repeat("a", 256)} {
		if _, err := SafeResourcePath(input); err == nil {
			t.Errorf("SafeResourcePath(%q) unexpectedly succeeded", input)
		}
	}
}

func TestSafeDirPathPreservesAbsoluteDirectories(t *testing.T) {
	got, err := SafeDirPath("/var/lib/apimgr")
	if err != nil {
		t.Fatalf("SafeDirPath returned error: %v", err)
	}
	if got != filepath.Clean("/var/lib/apimgr") {
		t.Fatalf("SafeDirPath = %q, want cleaned absolute path", got)
	}
}

func TestSafeDirPathRejectsTraversalAndControls(t *testing.T) {
	for _, input := range []string{"../outside", "/var/../etc", "/var/lib/\x00bad", "relative/\npath"} {
		if _, err := SafeDirPath(input); err == nil {
			t.Errorf("SafeDirPath(%q) unexpectedly succeeded", input)
		}
	}
}

func TestSafeFilePathKeepsPathWithinBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "data")
	got, err := SafeFilePath(base, "reports/daily")
	if err != nil {
		t.Fatalf("SafeFilePath returned error: %v", err)
	}
	want := filepath.Join(base, "reports", "daily")
	if got != want {
		t.Fatalf("SafeFilePath = %q, want %q", got, want)
	}
	if _, err := SafeFilePath(base, "../secret"); !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("SafeFilePath traversal error = %v, want %v", err, ErrPathTraversal)
	}
}

func TestSafeFilePathAcceptsExtensionsAndCase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "data")
	got, err := SafeFilePath(base, "Reports/Report_2024.PDF")
	if err != nil {
		t.Fatalf("SafeFilePath returned error: %v", err)
	}
	want := filepath.Join(base, "Reports", "Report_2024.PDF")
	if got != want {
		t.Fatalf("SafeFilePath = %q, want %q", got, want)
	}
}

func TestSafeFilePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "data")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := SafeFilePath(base, "linked/secret"); !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("SafeFilePath symlink escape error = %v, want %v", err, ErrPathTraversal)
	}
}

package newsletter

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFileUsesFrontMatterSubject(t *testing.T) {
	path := writeNewsletter(t, "issue.html", "---\nsubject: Front Matter Subject\nfrom_name: Custom Sender\n---\n<html><body>Hello</body></html>")

	parsed, err := ParseFile(path, "Default Sender")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if parsed.Subject != "Front Matter Subject" {
		t.Fatalf("Subject = %q", parsed.Subject)
	}
	if parsed.FromName != "Custom Sender" {
		t.Fatalf("FromName = %q", parsed.FromName)
	}
	if string(parsed.HTML) != "<html><body>Hello</body></html>" {
		t.Fatalf("HTML = %q", parsed.HTML)
	}
}

func TestParseFileFallsBackToTitle(t *testing.T) {
	path := writeNewsletter(t, "issue.html", "<html><head><title>Title Subject</title></head><body>Hello</body></html>")

	parsed, err := ParseFile(path, "Default Sender")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if parsed.Subject != "Title Subject" {
		t.Fatalf("Subject = %q", parsed.Subject)
	}
	if parsed.FromName != "Default Sender" {
		t.Fatalf("FromName = %q", parsed.FromName)
	}
}

func TestParseFileFallsBackToFilename(t *testing.T) {
	path := writeNewsletter(t, "weekly-update.html", "<html><body>Hello</body></html>")

	parsed, err := ParseFile(path, "Default Sender")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if parsed.Subject != "weekly-update" {
		t.Fatalf("Subject = %q", parsed.Subject)
	}
}

func TestParseFileHashesFullOriginalContents(t *testing.T) {
	contents := "---\nsubject: Hash Test\n---\n<html><body>Hello</body></html>"
	path := writeNewsletter(t, "hash.html", contents)
	wantHash := sha256.Sum256([]byte(contents))

	parsed, err := ParseFile(path, "Default Sender")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if parsed.SHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("SHA256 = %q", parsed.SHA256)
	}
}

func writeNewsletter(t *testing.T, name string, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

package newsletter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Parsed struct {
	Path     string
	SHA256   string
	Subject  string
	FromName string
	HTML     []byte
}

var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func ParseFile(path string, defaultFromName string) (Parsed, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Parsed{}, err
	}
	sum := sha256.Sum256(contents)
	meta, html := parseFrontMatter(contents)

	subject := meta["subject"]
	if subject == "" {
		subject = titleSubject(html)
	}
	if subject == "" {
		base := filepath.Base(path)
		subject = strings.TrimSuffix(base, filepath.Ext(base))
	}

	fromName := meta["from_name"]
	if fromName == "" {
		fromName = defaultFromName
	}

	return Parsed{
		Path:     path,
		SHA256:   hex.EncodeToString(sum[:]),
		Subject:  subject,
		FromName: fromName,
		HTML:     html,
	}, nil
}

func parseFrontMatter(contents []byte) (map[string]string, []byte) {
	meta := map[string]string{}
	if !bytes.HasPrefix(contents, []byte("---\n")) {
		return meta, contents
	}
	rest := contents[len("---\n"):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return meta, contents
	}
	for _, line := range strings.Split(string(rest[:end]), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return meta, rest[end+len("\n---\n"):]
}

func titleSubject(html []byte) string {
	matches := titlePattern.FindSubmatch(html)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(string(matches[1]))
}

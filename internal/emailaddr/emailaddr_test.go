package emailaddr

import "testing"

func TestNormalizeValidEmail(t *testing.T) {
	got, err := Normalize("  USER@Example.COM ")
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got != "user@example.com" {
		t.Fatalf("Normalize() = %q", got)
	}
}

func TestNormalizeRejectsInvalidEmail(t *testing.T) {
	for _, raw := range []string{"", "missing-at", "a@", "@example.com", "a b@example.com"} {
		if _, err := Normalize(raw); err == nil {
			t.Fatalf("Normalize(%q) expected error", raw)
		}
	}
}

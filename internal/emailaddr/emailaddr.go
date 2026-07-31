package emailaddr

import (
	"errors"
	"net/mail"
	"strings"
)

func Normalize(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("email is required")
	}

	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", errors.New("email is invalid")
	}
	if addr.Name != "" || addr.Address != trimmed {
		return "", errors.New("email must not include a display name")
	}

	parts := strings.Split(addr.Address, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("email is invalid")
	}
	if strings.ContainsAny(addr.Address, " \t\r\n") {
		return "", errors.New("email is invalid")
	}

	return strings.ToLower(addr.Address), nil
}

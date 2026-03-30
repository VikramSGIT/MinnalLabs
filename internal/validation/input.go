package validation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,64}$`)

func NormalizeUsername(username string) string {
	return strings.TrimSpace(username)
}

func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("username must be 3-64 characters and use only letters, numbers, and underscores")
	}
	return nil
}

func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if utf8.RuneCountInString(password) > 255 {
		return fmt.Errorf("password must be at most 255 characters")
	}
	return nil
}

func ValidateRequiredTrimmed(field, value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if maxRunes > 0 && utf8.RuneCountInString(value) > maxRunes {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxRunes)
	}
	return value, nil
}

func ValidateOptionalTrimmed(field, value string, maxRunes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if maxRunes > 0 && utf8.RuneCountInString(value) > maxRunes {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxRunes)
	}
	return value, nil
}

package validation

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

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

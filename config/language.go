package config

import (
	"aunefyren/poenskelisten/models"
	"errors"
	"slices"
	"strings"
)

// Languages are the languages the web app is translated into.
var Languages = []string{"en", "fr"}

// ParseDefaultLanguage checks the language the web app starts in for a
// visitor who hasn't chosen one: one of Languages, or "" or "auto" to follow
// each visitor's browser, returned as "".
func ParseDefaultLanguage(raw string) (string, error) {
	language := strings.ToLower(strings.TrimSpace(raw))
	if language == "" || language == "auto" {
		return "", nil
	}
	if !slices.Contains(Languages, language) {
		return "", errors.New("'" + raw + "' is not a language of the app; use " + strings.Join(Languages, ", ") + " or auto")
	}
	return language, nil
}

// DefaultLanguage is the configured default language, or "" to follow the
// browser. A wrong value, which only a hand-edited config.json can hold,
// counts as "".
func DefaultLanguage(configFile models.ConfigStruct) string {
	language, err := ParseDefaultLanguage(configFile.DefaultLanguage)
	if err != nil {
		return ""
	}
	return language
}

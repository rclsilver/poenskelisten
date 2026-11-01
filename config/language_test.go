package config

import (
	"aunefyren/poenskelisten/models"
	"testing"
)

func TestParseDefaultLanguage(t *testing.T) {
	for raw, want := range map[string]string{"en": "en", " FR ": "fr", "": "", "auto": "", "AUTO": ""} {
		if got, err := ParseDefaultLanguage(raw); err != nil || got != want {
			t.Errorf("ParseDefaultLanguage(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"de", "fr-FR", "français"} {
		if _, err := ParseDefaultLanguage(raw); err == nil {
			t.Errorf("ParseDefaultLanguage(%q) accepted a language the app isn't translated into", raw)
		}
	}
}

// Only a hand-edited config.json can hold a wrong value: pages then follow
// the browser rather than fail.
func TestDefaultLanguageIgnoresAWrongValue(t *testing.T) {
	if got := DefaultLanguage(models.ConfigStruct{DefaultLanguage: "de"}); got != "" {
		t.Errorf("DefaultLanguage(de) = %q, want \"\"", got)
	}
	if got := DefaultLanguage(models.ConfigStruct{DefaultLanguage: "fr"}); got != "fr" {
		t.Errorf("DefaultLanguage(fr) = %q, want fr", got)
	}
}

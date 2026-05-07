package i18n

import (
	"testing"
)

func TestNewFallsBackToDefault(t *testing.T) {
	tr := New("klingon")
	if tr.Locale() != DefaultLocale {
		t.Fatalf("got %q, want %q", tr.Locale(), DefaultLocale)
	}
}

func TestT(t *testing.T) {
	en := New("en")
	pl := New("pl")

	if got := en.T(LedgerNowOwes, "U1", "20.00", "PLN"); got != "<@U1> now owes you 20.00 PLN." {
		t.Errorf("en: %q", got)
	}
	if got := pl.T(LedgerNowOwes, "U1", "20.00", "PLN"); got != "<@U1> jest tobie winny 20.00 PLN." {
		t.Errorf("pl: %q", got)
	}
}

func TestEveryKeyTranslatedInEveryLocale(t *testing.T) {
	enKeys := bundles[DefaultLocale]
	for locale, msgs := range bundles {
		for k := range enKeys {
			if _, ok := msgs[k]; !ok {
				t.Errorf("locale %q is missing key %q", locale, k)
			}
		}
		for k := range msgs {
			if _, ok := enKeys[k]; !ok {
				t.Errorf("locale %q has stray key %q (not in en)", locale, k)
			}
		}
	}
}

func TestMissingKeyFallsBackToEnglish(t *testing.T) {
	tr := &Translator{locale: "pl", msgs: map[string]string{}}
	if want := "Nobody owes you and you owe nothing."; tr.T(LedgerStatusAllEmpty) != want {
		t.Fatalf("expected English fallback %q, got %q", want, tr.T(LedgerStatusAllEmpty))
	}
}

func TestUnknownKeyReturnsID(t *testing.T) {
	if got := New("en").T("nonexistent.key"); got != "nonexistent.key" {
		t.Fatalf("got %q", got)
	}
}

func TestAvailable(t *testing.T) {
	got := Available()
	if len(got) != len(bundles) {
		t.Fatalf("len = %d, want %d", len(got), len(bundles))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("not sorted: %v", got)
		}
	}
	for _, locale := range got {
		if _, ok := bundles[locale]; !ok {
			t.Errorf("returned %q not in bundles", locale)
		}
	}
}

package utils

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateStringKeepsUTF8Boundary(t *testing.T) {
	input := "你好世界"
	got := TruncateString(input, 5)

	if got != "你" {
		t.Fatalf("expected %q, got %q", "你", got)
	}

	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8, got %q", got)
	}
}

func TestTruncateStringNormalizesInvalidUTF8(t *testing.T) {
	input := string([]byte{0xe5, 0xa5, 0xbd, 0xe5})
	got := TruncateString(input, 16)

	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8, got %q", got)
	}

	if got != "好�" {
		t.Fatalf("expected %q, got %q", "好�", got)
	}
}

func TestNormalizeUTF8(t *testing.T) {
	input := string([]byte{0xff, 'a'})
	got := NormalizeUTF8(input)

	if got != "�a" {
		t.Fatalf("expected %q, got %q", "�a", got)
	}
}

// TestRedactSecret asserts that every occurrence of a known secret inside an
// arbitrary text is replaced with its masked form. Regression test for the
// Gemini-channel upstream key leak via unmasked transport errors (CWE-200).
func TestRedactSecret(t *testing.T) {
	secret := "sk-secret-upstream-key-1234"
	text := `Post "http://upstream/v1beta/models/gemini-pro:generateContent?key=sk-secret-upstream-key-1234": context deadline exceeded`

	got := RedactSecret(text, secret)

	if got == text {
		t.Fatalf("RedactSecret did not modify the text: %q", got)
	}
	for i := 0; i+len(secret) <= len(got); i++ {
		if got[i:i+len(secret)] == secret {
			t.Fatalf("raw secret still present in redacted text: %q", got)
		}
	}
	want := `Post "http://upstream/v1beta/models/gemini-pro:generateContent?key=sk-s****1234": context deadline exceeded`
	if got != want {
		t.Errorf("RedactSecret() = %q, want %q", got, want)
	}
}

// TestRedactSecretMultipleOccurrences asserts all occurrences are redacted, not just the first.
func TestRedactSecretMultipleOccurrences(t *testing.T) {
	secret := "sk-secret-upstream-key-1234"
	text := secret + " appears twice: " + secret

	got := RedactSecret(text, secret)

	masked := MaskAPIKey(secret)
	want := masked + " appears twice: " + masked
	if got != want {
		t.Errorf("RedactSecret() = %q, want %q", got, want)
	}
}

// TestRedactSecretEmptySecret asserts an empty secret leaves the text untouched
// (guards against strings.ReplaceAll's pathological behavior on an empty old value).
func TestRedactSecretEmptySecret(t *testing.T) {
	text := "some upstream error with no secret"

	got := RedactSecret(text, "")

	if got != text {
		t.Errorf("RedactSecret() with empty secret = %q, want unchanged %q", got, text)
	}
}

// TestRedactSecretNotPresent asserts text without the secret is returned unchanged.
func TestRedactSecretNotPresent(t *testing.T) {
	text := "connection refused"

	got := RedactSecret(text, "sk-secret-upstream-key-1234")

	if got != text {
		t.Errorf("RedactSecret() = %q, want unchanged %q", got, text)
	}
}

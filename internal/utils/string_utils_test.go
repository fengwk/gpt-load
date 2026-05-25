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

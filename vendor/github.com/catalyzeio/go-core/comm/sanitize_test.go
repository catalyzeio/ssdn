package comm

import (
	"testing"
)

func TestSanitizeService(t *testing.T) {
	input := "asd;fli u230 [0a sjdfasdf!\u2026asdf"
	expected := "asd_fli_u230__0a_sjdfasdf__asdf"

	sanitized := SanitizeService(input)
	if sanitized != expected {
		t.Error("mismatch", sanitized, expected)
	}
}

func TestSanitizeServices(t *testing.T) {
	input := []string{"fo:o!=", "bar##", " baz", "buzz"}
	expected := []string{"fo_o__", "bar__", "_baz", "buzz"}

	sanitized := SanitizeServices(input)
	for i := range input {
		if sanitized[i] != expected[i] {
			t.Error("mismatch", sanitized[i], expected[i])
		}
	}
}

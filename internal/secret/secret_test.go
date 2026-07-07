package secret

import "testing"

func TestMask(t *testing.T) {
	if got := Mask("abcdef123456"); got != "ab****56" {
		t.Fatalf("Mask() = %q", got)
	}
	if got := Mask("abc"); got != "****" {
		t.Fatalf("Mask(short) = %q", got)
	}
}

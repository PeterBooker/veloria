package auth

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	raw, hash, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}

	// Raw token has the expected prefix and length.
	if !strings.HasPrefix(raw, tokenPrefix) {
		t.Errorf("raw token %q does not start with %q", raw, tokenPrefix)
	}
	// "vel_" (4) + hex(32 bytes) (64) = 68 chars
	if len(raw) != 68 {
		t.Errorf("raw token length = %d, want 68", len(raw))
	}

	// Hash is a 64-char hex SHA-256 digest.
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}

	// Hash is deterministic for the same raw token.
	if got := hashToken(raw); got != hash {
		t.Errorf("hashToken(raw) = %q, want %q", got, hash)
	}
}

func TestGenerateToken_Uniqueness(t *testing.T) {
	raw1, _, _ := GenerateToken()
	raw2, _, _ := GenerateToken()
	if raw1 == raw2 {
		t.Error("two generated tokens should not be equal")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	h1 := hashToken("vel_abc123")
	h2 := hashToken("vel_abc123")
	if h1 != h2 {
		t.Error("hashToken should be deterministic")
	}
}

func TestHashToken_DifferentInputs(t *testing.T) {
	h1 := hashToken("vel_abc123")
	h2 := hashToken("vel_def456")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestTokenSuffix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"vel_abcdefghijklmnop", "ijklmnop"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"123456789", "23456789"},
	}
	for _, tt := range tests {
		if got := tokenSuffix(tt.input); got != tt.want {
			t.Errorf("tokenSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

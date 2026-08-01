package security

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// RFC 6238 publishes test vectors for the secret "12345678901234567890".
// Checking against them proves the implementation is interoperable with real
// authenticator apps rather than merely self-consistent.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}

	cases := map[int64]string{
		59:         "287082",
		1111111109: "081804",
		1111111111: "050471",
		1234567890: "005924",
		2000000000: "279037",
	}

	for unix, want := range cases {
		counter := unix / int64(totpPeriod.Seconds())
		if got := totpCode(key, counter); got != want {
			t.Errorf("at %d: got %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyTOTPAcceptsCurrentCode(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("new secret: %v", err)
	}

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	counter := time.Now().Unix() / int64(totpPeriod.Seconds())

	if !VerifyTOTP(secret, totpCode(key, counter)) {
		t.Error("the current code must be accepted")
	}

	// One step either way covers a slightly wrong client clock.
	if !VerifyTOTP(secret, totpCode(key, counter-1)) {
		t.Error("the previous code must still be accepted")
	}

	if VerifyTOTP(secret, totpCode(key, counter+5)) {
		t.Error("a code five steps away must be rejected")
	}
}

func TestVerifyTOTPRejectsMalformedInput(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("new secret: %v", err)
	}

	for _, code := range []string{"", "12345", "1234567", "abcdef"} {
		if VerifyTOTP(secret, code) {
			t.Errorf("%q must be rejected", code)
		}
	}

	if VerifyTOTP("not-valid-base32!", "123456") {
		t.Error("an undecodable secret must be rejected")
	}
}

func TestTOTPSecretsAreDistinct(t *testing.T) {
	seen := make(map[string]bool)

	for range 20 {
		secret, err := NewTOTPSecret()
		if err != nil {
			t.Fatalf("new secret: %v", err)
		}

		if seen[secret] {
			t.Fatal("secrets must not repeat")
		}

		seen[secret] = true
	}
}

func TestTOTPURIIsScannable(t *testing.T) {
	uri := TOTPURI("Zeiterfassung", "admin@local", "ABCDEF")

	for _, want := range []string{"otpauth://totp/", "secret=ABCDEF", "issuer=Zeiterfassung", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Errorf("URI %q is missing %q", uri, want)
		}
	}
}

func TestSessionTokensAreDistinctAndHashed(t *testing.T) {
	first, err := NewSessionToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}

	second, err := NewSessionToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}

	if first == second {
		t.Fatal("session tokens must not repeat")
	}

	if HashToken(first) == first {
		t.Error("the stored form must not equal the token itself")
	}

	// Separate variables, not two calls compared inline: the compiler and the
	// linter both treat `f(x) != f(x)` as trivially false.
	firstHash := HashToken(first)
	secondHash := HashToken(first)

	if firstHash != secondHash {
		t.Error("hashing must be deterministic")
	}

	if firstHash == HashToken(second) {
		t.Error("different tokens must not hash alike")
	}
}

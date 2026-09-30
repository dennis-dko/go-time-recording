package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 mandates HMAC-SHA1 for interoperability
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters. These are the values every authenticator app assumes by
// default, so deviating would break scanning the secret into them.
const (
	totpDigits = 6
	totpPeriod = 30 * time.Second

	// totpSkew accepts the neighbouring steps as well, covering clocks that
	// drift by up to one period in either direction.
	totpSkew = 1

	totpSecretBytes = 20 // 160 bits, as recommended by RFC 4226
)

// NewTOTPSecret returns a fresh base32 secret for an authenticator app.
func NewTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	// No padding: authenticator apps reject the '=' characters.
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// TOTPURI builds the otpauth:// URI an authenticator app expects.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)

	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(totpDigits))
	query.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))

	return "otpauth://totp/" + label + "?" + query.Encode()
}

// MatchTOTP reports whether code is currently valid for secret, and which time
// step it belongs to.
//
// The neighbouring time steps are accepted too, so a client whose clock is
// slightly off still works. The step is what a caller needs to accept a code
// once only: RFC 6238 forbids accepting the same code a second time (section
// 5.2), and a code is good for three steps, so without it one code opened
// every sign-in for a minute and a half.
func MatchTOTP(secret, code string) (step int64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return 0, false
	}

	counter := time.Now().Unix() / int64(totpPeriod.Seconds())

	for offset := -totpSkew; offset <= totpSkew; offset++ {
		expected := totpCode(key, counter+int64(offset))

		// Constant time: a timing difference would leak how much of the code
		// was correct, which is enough to guess it digit by digit.
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return counter + int64(offset), true
		}
	}

	return 0, false
}

// CurrentTOTPCode returns the code an authenticator app would be showing for
// secret right now.
//
// The counterpart to MatchTOTP, and the side an authenticator normally plays.
// Exported so a test can sign in with two-factor enabled without reimplementing
// RFC 6238, which would only prove that the copy agrees with itself.
func CurrentTOTPCode(secret string) (string, error) {
	return TOTPCodeAt(secret, time.Now())
}

// TOTPCodeAt returns the code an authenticator app would show for secret at the
// given moment. A test that uses a second code soon after a first asks for the
// next step's, which is accepted now and is not the one already spent.
func TOTPCodeAt(secret string, at time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}

	return totpCode(key, at.Unix()/int64(totpPeriod.Seconds())), nil
}

// totpCode implements the HOTP truncation of RFC 4226 for one counter value.
func totpCode(key []byte, counter int64) string {
	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], uint64(counter))

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation: the low nibble of the last byte picks the offset.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}

	return fmt.Sprintf("%0*d", totpDigits, value%mod)
}

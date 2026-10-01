package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// stallAfter is how long the download may go without a byte arriving: thirty
// seconds, the patience http.DefaultTransport gives a connection to be
// established at all. On a link that works, nothing arriving for that long is a
// link that has stopped, however slow the download was going.
const stallAfter = 30 * time.Second

// patience is stallAfter, or a test's shorter override.
func (s *Source) patience() time.Duration {
	if s.stall > 0 {
		return s.stall
	}

	return stallAfter
}

// dialStalling connects the way http.DefaultTransport does, with its connect
// timeout, and hands back a connection that gives up once reads stop arriving.
func (s *Source) dialStalling(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: stallAfter}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	return stallingConn{Conn: conn, after: s.patience()}, nil
}

// stallingConn gives every read a fresh deadline, so a connection that stops
// delivering is cut off after that long with nothing, however long it has been
// open - progress bounds it, not duration. It sits under TLS and HTTP/2 alike,
// and on a connection left idle in the pool it closes it, which is what an idle
// timeout would have done.
type stallingConn struct {
	net.Conn
	after time.Duration
}

func (c stallingConn) Read(b []byte) (int, error) {
	if err := c.SetReadDeadline(time.Now().Add(c.after)); err != nil {
		return 0, err
	}

	return c.Conn.Read(b)
}

// lookupTimeout bounds the calls that fetch a small document, and the handshake
// and header phases of the one that does not.
const lookupTimeout = 10 * time.Second

// downloadClient is who fetches the binary.
func (s *Source) downloadClient() *http.Client {
	if s.Downloader != nil {
		return s.Downloader
	}

	return s.Client
}

// checksum reads the published hash for this platform's asset.
func (s *Source) checksum(ctx context.Context, release Release) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.sums, nil)
	if err != nil {
		return "", err
	}

	res, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot read the published checksums: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the checksums answered %d", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return "", err
	}

	wanted := assetName(release.Version)

	for line := range strings.SplitSeq(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		// sha256sum writes "hash  name" and marks a binary read with a leading
		// asterisk on the name.
		if strings.TrimPrefix(fields[1], "*") == wanted {
			return strings.ToLower(fields[0]), nil
		}
	}

	return "", fmt.Errorf("the published checksums do not mention %s", wanted)
}

// sumOf is the SHA-256 of a file, in the form the published checksums use.
func sumOf(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer func() { _ = file.Close() }()

	sum := sha256.New()

	if _, err := io.Copy(sum, io.LimitReader(file, maxDownload+1)); err != nil {
		return "", err
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// download fetches the asset and writes it only if it hashes to want.
func (s *Source) download(ctx context.Context, url, into, want string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	res, err := s.downloadClient().Do(req)
	if err != nil {
		return fmt.Errorf("cannot download the release: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the download answered %d", res.StatusCode)
	}

	// 0o755 rather than 0o644: this is about to be the application.
	file, err := os.OpenFile(into, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write beside the current binary: %w", err)
	}

	sum := sha256.New()

	// Hashed while it is written rather than read back afterwards, so the bytes
	// that were checked are the bytes that landed.
	if _, err := io.Copy(io.MultiWriter(file, sum), io.LimitReader(res.Body, maxDownload)); err != nil {
		_ = file.Close()

		if errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("the download stopped arriving - nothing came for %s: %w",
				s.patience(), err)
		}

		return fmt.Errorf("the download broke off: %w", err)
	}

	if err := file.Close(); err != nil {
		return err
	}

	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return fmt.Errorf("the download does not match the published checksum "+
			"(got %s, expected %s)", got[:16], want[:16])
	}

	return nil
}

// maxDownload bounds what will be written to disk.
//
// The published binaries are 45 to 50 MB - measured on v0.2.46, where the four
// assets run from 45.5 to 49.7 MB - so a hundred is a little over twice the
// current size. That was written down as "around thirty megabytes" and is worth
// keeping honest, because this bound is the one that turns into a failed update
// rather than a refused request if the binary keeps growing: at the present rate
// it is the number to revisit before it is the number that breaks.
const maxDownload = 100 << 20

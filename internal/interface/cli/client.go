package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// requestTimeout bounds one request: a terminal waiting on an installation that
// has gone away should say so rather than hang until somebody presses Ctrl-C.
const requestTimeout = 30 * time.Second

// maxAnswer bounds how much of an answer is read. The largest this asks for is a
// day's entries, which is a few kilobytes; anything near this is not one of this
// application's answers.
const maxAnswer = 8 << 20

// client asks one installation's API as the owner of one personal token, and
// says what went wrong in the language track speaks.
type client struct {
	base  string
	token string
	http  *http.Client
	lang  language
}

// newClient is a client for the installation at base, which may end in a slash.
func newClient(base, token string, lang language) *client {
	return &client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: requestTimeout},
		lang:  lang,
	}
}

// refusal is what the installation answered instead of doing something, in its
// own words - which are what the person at the terminal needs to read.
type refusal string

func (r refusal) Error() string { return string(r) }

// call sends a request and reads the data of the answer into into, nil for an
// answer nobody needs.
func (c *client) call(ctx context.Context, method, path string, body, into any) error {
	var payload io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}

		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, payload)
	if err != nil {
		return fmt.Errorf("%s: %w", c.lang.text("track.badAddress", "\"{0}\" is not an address this can ask", c.base), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", c.lang.text("track.unreachable", "could not reach {0}", c.base), err)
	}

	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("%s: %w", c.lang.text("track.unreadable", "reading the answer from {0}", c.base), err)
	}

	if resp.StatusCode >= http.StatusMultipleChoices {
		return c.lang.refusal(resp.StatusCode, raw)
	}

	if into == nil {
		return nil
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Data == nil {
		return errors.New(c.lang.text("track.notThisApplication", "{0} answered with something that is not this application's", c.base))
	}

	return json.Unmarshal(envelope.Data, into)
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

// client asks one installation's API as the owner of one personal token.
type client struct {
	base  string
	token string
	http  *http.Client
}

// newClient is a client for the installation at base, which may end in a slash.
func newClient(base, token string) *client {
	return &client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: requestTimeout},
	}
}

// refusal is what the installation answered instead of doing something, in its
// own words - which are what the person at the terminal needs to read.
type refusal struct {
	status  int
	message string
}

func (r refusal) Error() string { return r.message }

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
		return fmt.Errorf("%q is not an address this can ask: %w", c.base, err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", c.base, err)
	}

	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("reading the answer from %s: %w", c.base, err)
	}

	if resp.StatusCode >= http.StatusMultipleChoices {
		return refusalOf(resp.StatusCode, raw)
	}

	if into == nil {
		return nil
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Data == nil {
		return fmt.Errorf("%s answered with something that is not this application's", c.base)
	}

	return json.Unmarshal(envelope.Data, into)
}

// refusalOf reads the installation's own sentence out of a refused request.
//
// Not signed in is said in this program's words instead, because the
// installation's are about a session and what is wrong here is the token.
func refusalOf(status int, raw []byte) error {
	if status == http.StatusUnauthorized {
		return refusal{status: status,
			message: "the installation does not know this token - make one under My account, API tokens, and put it in GTR_TOKEN"}
	}

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	message := ""
	if json.Unmarshal(raw, &body) == nil {
		message = body.Error.Message
	}

	if message == "" {
		message = fmt.Sprintf("the installation answered %d %s", status, http.StatusText(status))
	}

	return refusal{status: status, message: message}
}

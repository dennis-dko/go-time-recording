//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// A new token comes with what an app needs to use it, once: the address this
// installation was reached at and the token itself - the two values the
// terminal client is given as GTR_URL and GTR_TOKEN - as a
// go-time-recording://connect address and as a QR code of exactly that address.
// The address opens the API as the token's owner, and the list of tokens never
// shows any of it again.
func TestANewTokenComesWithACodeAnAppCanScan(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	var created struct {
		Secret     string `json:"secret"`
		ConnectURI string `json:"connectUri"`
		QR         string `json:"qr"`
	}

	worker.must(worker.api(http.MethodPost, "/me/tokens", map[string]any{"name": "phone"}),
		http.StatusCreated, http.StatusOK).Data(t, &created)

	connect, err := url.Parse(created.ConnectURI)
	if err != nil || connect.Scheme != "go-time-recording" || connect.Host != "connect" {
		t.Fatalf("the new token came with %q, want a go-time-recording://connect address", created.ConnectURI)
	}

	address, token := connect.Query().Get("url"), connect.Query().Get("token")

	if address != a.BaseURL() {
		t.Errorf("the address names the installation as %q; it was reached at %q", address, a.BaseURL())
	}

	if token == "" || token != created.Secret {
		t.Errorf("the address carries the token %q, and the token is %q", token, created.Secret)
	}

	if want, err := qrcode.SVGDataURI(created.ConnectURI); err != nil || created.QR != want {
		t.Errorf("the QR code that came with the token is not the code of its address (%d characters, want %d)",
			len(created.QR), len(want))
	}

	// What an app does with it: the address and the token, and nothing else.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address+"/api/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("asking with what the address carries: %v", err)
	}

	_ = response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Errorf("what the address carries was answered %d", response.StatusCode)
	}

	var listed listOf[map[string]any]

	worker.must(worker.api(http.MethodGet, "/me/tokens", nil), http.StatusOK).Data(t, &listed)

	if len(listed.Items) == 0 {
		t.Fatal("the list holds no token, so nothing is checked against it")
	}

	for _, listedToken := range listed.Items {
		for _, field := range []string{"secret", "connectUri", "qr"} {
			if _, shown := listedToken[field]; shown {
				t.Errorf("the list of tokens shows %s, which exists only in the answer that made the token", field)
			}
		}
	}
}

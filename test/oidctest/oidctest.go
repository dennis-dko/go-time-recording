package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// ClientID and ClientSecret are what the provider knows the application as.
const (
	ClientID     = "go-time-recording"
	ClientSecret = "oidctest-client-secret"
)

// keyID names the provider's signing key in its key set and in every token.
const keyID = "oidctest"

// Person is who the provider says signs in.
type Person struct {
	Subject string
	Email   string
	Name    string
}

// Provider is a provider serving one client, with a key of its own.
type Provider struct {
	// URL is its issuer: where its discovery document is read, and what every
	// token it signs names.
	URL string

	key *rsa.PrivateKey

	mu     sync.Mutex
	next   Person
	grants map[string]grant

	// tamper changes the claims of the next token, and forge signs it with a key
	// nobody published - how a test makes a token the application has to refuse.
	tamper func(claims map[string]any)
	forge  bool

	exchanged int
}

// grant is a code the provider has handed out and not yet traded.
type grant struct {
	person      Person
	nonce       string
	challenge   string
	redirectURI string
}

// Start serves a provider until the test ends. Until SignInAs says otherwise, it
// signs in Wera, as subject "wera-subject".
func Start(t testing.TB) *Provider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("make the provider's key: %v", err)
	}

	p := &Provider{
		key:    key,
		next:   Person{Subject: "wera-subject", Email: "wera@example.com", Name: "Wera"},
		grants: map[string]grant{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// By name rather than by the address it listens on: the browser suite lets
	// Chrome resolve nothing but localhost, and that rule turns 127.0.0.1 away as
	// well, so a provider the browser is sent to has to be called localhost.
	p.URL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	return p
}

// SignInAs says who every authorisation from now on signs in.
func (p *Provider) SignInAs(person Person) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.next = person
}

// TamperWithNextToken changes the claims of the next token the provider signs.
func (p *Provider) TamperWithNextToken(change func(claims map[string]any)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tamper = change
}

// ForgeNextToken signs the next token with a key the provider never published,
// under the name of the one it did.
func (p *Provider) ForgeNextToken() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.forge = true
}

// Exchanged reports how many codes have been traded for a token, so a test can
// tell a refusal that came before the provider was asked from one that came after.
func (p *Provider) Exchanged() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.exchanged
}

// Follow does what a browser does with the address an application sends it to,
// up to the moment it would return: it asks the provider, and answers with where
// the provider sends it back.
func Follow(t testing.TB, address string) *url.URL {
	t.Helper()

	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp, err := client.Get(address) //nolint:noctx // a test following a redirect by hand
	if err != nil {
		t.Fatalf("ask the provider: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the provider answered %d rather than sending the browser back", resp.StatusCode)
	}

	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("read where the provider sends the browser: %v", err)
	}

	return back
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	answer(w, http.StatusOK, map[string]any{
		"issuer":                                p.URL,
		"authorization_endpoint":                p.URL + "/authorize",
		"token_endpoint":                        p.URL + "/token",
		"jwks_uri":                              p.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) keys(w http.ResponseWriter, _ *http.Request) {
	answer(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}})
}

// authorize sends the browser straight back with a code: who signs in is what
// SignInAs said, not what anybody typed into a form.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	asked := r.URL.Query()

	if asked.Get("client_id") != ClientID || asked.Get("response_type") != "code" ||
		asked.Get("code_challenge_method") != "S256" || asked.Get("code_challenge") == "" {
		http.Error(w, "not a request this provider answers", http.StatusBadRequest)

		return
	}

	back, err := url.Parse(asked.Get("redirect_uri"))
	if err != nil || back.Scheme == "" {
		http.Error(w, "no address to send the browser back to", http.StatusBadRequest)

		return
	}

	code := random()

	p.mu.Lock()
	p.grants[code] = grant{
		person: p.next, nonce: asked.Get("nonce"),
		challenge: asked.Get("code_challenge"), redirectURI: asked.Get("redirect_uri"),
	}
	p.mu.Unlock()

	query := back.Query()
	query.Set("code", code)
	query.Set("state", asked.Get("state"))
	back.RawQuery = query.Encode()

	http.Redirect(w, r, back.String(), http.StatusFound)
}

// token trades a code for an identity token: once, for the client it was given to,
// for the address it was given for, and only with the verifier its challenge was
// made from.
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		answer(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})

		return
	}

	if !clientAuthenticated(r) {
		answer(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})

		return
	}

	p.mu.Lock()
	given, found := p.grants[r.PostForm.Get("code")]
	delete(p.grants, r.PostForm.Get("code"))
	p.mu.Unlock()

	verified := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))

	if !found || r.PostForm.Get("grant_type") != "authorization_code" ||
		r.PostForm.Get("redirect_uri") != given.redirectURI ||
		base64.RawURLEncoding.EncodeToString(verified[:]) != given.challenge {
		answer(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})

		return
	}

	token, err := p.sign(given)
	if err != nil {
		answer(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})

		return
	}

	p.mu.Lock()
	p.exchanged++
	p.mu.Unlock()

	answer(w, http.StatusOK, map[string]any{
		"access_token": random(), "token_type": "Bearer", "expires_in": 300, "id_token": token,
	})
}

// clientAuthenticated reports whether the request names this provider's client
// and its secret, in either of the two ways a client may send them.
func clientAuthenticated(r *http.Request) bool {
	id, secret, basic := r.BasicAuth()
	if basic {
		// A client sending them as a header escapes them first, as the
		// specification asks (RFC 6749, 2.3.1).
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}

	return id == ClientID && secret == ClientSecret
}

// sign makes the identity token for a grant, tampered with or forged if a test
// asked for that.
func (p *Provider) sign(given grant) (string, error) {
	now := time.Now()

	claims := map[string]any{
		"iss": p.URL, "sub": given.person.Subject, "aud": ClientID,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": given.nonce,
		"email": given.person.Email, "email_verified": true, "name": given.person.Name,
	}

	p.mu.Lock()
	tamper, forge := p.tamper, p.forge
	p.tamper, p.forge = nil, false
	p.mu.Unlock()

	if tamper != nil {
		tamper(claims)
	}

	key := p.key

	if forge {
		var err error
		if key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			return "", err
		}
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: keyID}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}

	return signed.CompactSerialize()
}

func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func random() string {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)

	return base64.RawURLEncoding.EncodeToString(raw)
}

// Package directory authenticates users against an LDAP directory.
//
// It implements service.ExternalAuthenticator, so the application layer never
// sees the LDAP client and can be tested without a directory.
package directory

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// dialTimeout bounds how long a sign-in waits on an unreachable directory.
const dialTimeout = 10 * time.Second

// LDAP authenticates against a directory using the configuration the
// administrator saved.
type LDAP struct {
	mu     sync.RWMutex
	config model.LDAPConfig
}

// New creates an authenticator with no configuration; call Configure once the
// stored settings have been read.
func New() *LDAP {
	return &LDAP{}
}

var _ appservice.ExternalAuthenticator = (*LDAP)(nil)

// Configure replaces the connection settings. It is safe to call while the
// application is serving, so saving the settings screen takes effect at once.
func (l *LDAP) Configure(config model.LDAPConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.config = config
}

// Enabled reports whether a directory is configured.
func (l *LDAP) Enabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.config.Enabled && l.config.Host != ""
}

// Authenticate verifies the credentials against the directory.
//
// The boolean says whether this directory recognised the user at all. A false
// result with no error lets the caller fall back to a local password, so
// local-only accounts keep working next to directory ones.
func (l *LDAP) Authenticate(
	ctx context.Context,
	login, password string,
) (*appservice.ExternalUser, bool, error) {
	l.mu.RLock()
	config := l.config
	l.mu.RUnlock()

	if !config.Enabled || config.Host == "" {
		return nil, false, nil
	}

	// An empty password would be an unauthenticated bind, which most
	// directories accept and which would let anyone in as anyone.
	if password == "" {
		return nil, false, nil
	}

	conn, err := dial(ctx, config)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = conn.Close() }()

	entry, err := findUser(conn, config, login)
	if err != nil {
		return nil, false, err
	}

	if entry == nil {
		return nil, false, nil
	}

	// The actual password check: rebinding as the user is the only way to
	// verify it, since the directory never hands the hash out.
	if err := conn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, false, nil
		}

		return nil, false, err
	}

	email := entry.GetAttributeValue(config.EmailAttribute)
	if email == "" {
		// Without a mail address there is no stable local identifier, so the
		// login is used instead.
		email = login
	}

	return &appservice.ExternalUser{
		ID:    stableID(entry, config.IDAttribute),
		Email: strings.ToLower(email),
		Name:  entry.GetAttributeValue(config.NameAttribute),
	}, true, nil
}

// ListUsers returns the mail addresses of every account the directory holds
// under the configured base DN.
//
// This is a pure read: the directory is never written to. The result drives
// the synchronisation, so an incomplete answer would look like "these people
// left" - which is why a failed search returns an error rather than a short
// list, and the caller refuses to act on an empty one.
func (l *LDAP) ListUsers(ctx context.Context) ([]appservice.ExternalUser, error) {
	l.mu.RLock()
	config := l.config
	l.mu.RUnlock()

	if !config.Enabled || config.Host == "" {
		return nil, fmt.Errorf("no directory is configured")
	}

	conn, err := dial(ctx, config)
	if err != nil {
		return nil, err
	}

	defer func() { _ = conn.Close() }()

	if config.BindDN != "" {
		if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
			return nil, fmt.Errorf("service bind failed: %w", err)
		}
	}

	// The login filter matches one person; listing everyone means replacing
	// the placeholder with a wildcard.
	filter := strings.ReplaceAll(config.UserFilter, "%s", "*")

	// Paged, because directories commonly cap a plain search at 500 or 1000
	// entries and would otherwise silently truncate - which here would read
	// as "everyone beyond the first page has left".
	result, err := conn.SearchWithPaging(ldap.NewSearchRequest(
		config.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		0, 0, false,
		filter, requestedAttributes(config), nil), syncPageSize)
	if err != nil {
		return nil, fmt.Errorf("listing directory users failed: %w", err)
	}

	users := make([]appservice.ExternalUser, 0, len(result.Entries))

	for _, entry := range result.Entries {
		email := entry.GetAttributeValue(config.EmailAttribute)
		if email == "" {
			// Without a mail address the entry cannot be matched to a local
			// account, so it is skipped rather than guessed at.
			continue
		}

		users = append(users, appservice.ExternalUser{
			ID:    stableID(entry, config.IDAttribute),
			Email: strings.ToLower(email),
			Name:  entry.GetAttributeValue(config.NameAttribute),
		})
	}

	return users, nil
}

// syncPageSize is the LDAP paging size used while listing users.
const syncPageSize = 500

// stableID reads the identifier that outlives a rename.
//
// Active Directory's objectGUID is a raw 16-byte value that is not valid
// text, so any binary attribute is hex-encoded rather than being mangled by a
// string conversion. OpenLDAP's entryUUID is already printable and passes
// through unchanged.
func stableID(entry *ldap.Entry, attribute string) string {
	if attribute == "" {
		return ""
	}

	raw := entry.GetRawAttributeValue(attribute)
	if len(raw) == 0 {
		return ""
	}

	if printable(raw) {
		return string(raw)
	}

	return hex.EncodeToString(raw)
}

// printable reports whether the bytes are safe to keep as text.
func printable(raw []byte) bool {
	for _, b := range raw {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}

	return true
}

// requestedAttributes lists what a search should return, skipping blanks so a
// directory is not asked for an attribute named "".
func requestedAttributes(config model.LDAPConfig) []string {
	attributes := []string{"dn"}

	for _, name := range []string{config.NameAttribute, config.EmailAttribute, config.IDAttribute} {
		if name != "" {
			attributes = append(attributes, name)
		}
	}

	return attributes
}

// TestConnection checks the settings without signing anyone in, so the
// administrator gets a straight answer from the settings screen.
func (l *LDAP) TestConnection(ctx context.Context, config model.LDAPConfig) error {
	conn, err := dial(ctx, config)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if config.BindDN != "" {
		if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
			return fmt.Errorf("bind as %q failed: %w", config.BindDN, err)
		}
	}

	// A search proves the base DN is usable, which a bind alone does not.
	_, err = conn.Search(ldap.NewSearchRequest(
		config.BaseDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases,
		1, int(dialTimeout.Seconds()), false,
		"(objectClass=*)", []string{"dn"}, nil))
	if err != nil {
		return fmt.Errorf("searching base DN %q failed: %w", config.BaseDN, err)
	}

	return nil
}

func dial(ctx context.Context, config model.LDAPConfig) (*ldap.Conn, error) {
	address := fmt.Sprintf("%s:%d", config.Host, config.Port)

	tlsConfig := &tls.Config{
		ServerName: config.Host,
		//nolint:gosec // opt-in, for self-signed directories in test setups
		InsecureSkipVerify: config.SkipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	scheme := "ldap"
	if config.UseTLS {
		scheme = "ldaps"
	}

	// The client has no context-aware dial, so the deadline is applied to the
	// underlying dialer instead. A caller that cancels early still returns as
	// soon as this bound elapses.
	deadline := dialTimeout
	if until, ok := ctx.Deadline(); ok {
		if remaining := time.Until(until); remaining > 0 && remaining < deadline {
			deadline = remaining
		}
	}

	conn, err := ldap.DialURL(fmt.Sprintf("%s://%s", scheme, address),
		ldap.DialWithTLSConfig(tlsConfig),
		ldap.DialWithDialer(&net.Dialer{Timeout: deadline}))
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", address, err)
	}

	conn.SetTimeout(deadline)

	if config.StartTLS && !config.UseTLS {
		if err := conn.StartTLS(tlsConfig); err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("StartTLS failed: %w", err)
		}
	}

	return conn, nil
}

// findUser locates the account, binding as the service account first when one
// is configured.
func findUser(conn *ldap.Conn, config model.LDAPConfig, login string) (*ldap.Entry, error) {
	if config.BindDN != "" {
		if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
			return nil, fmt.Errorf("service bind failed: %w", err)
		}
	}

	// EscapeFilter guards against a login crafted to alter the filter.
	safe := ldap.EscapeFilter(login)
	filter := strings.ReplaceAll(config.UserFilter, "%s", safe)

	result, err := conn.Search(ldap.NewSearchRequest(
		config.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, int(dialTimeout.Seconds()), false,
		filter, requestedAttributes(config), nil))
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// More than one match means the filter is ambiguous; signing in as "one of
	// them" would be a guess.
	if len(result.Entries) != 1 {
		return nil, nil
	}

	return result.Entries[0], nil
}

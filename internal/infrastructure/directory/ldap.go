package directory

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"strconv"
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

var _ appservice.ExternalDirectory = (*LDAP)(nil)

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

	user, err := signedIn(entry, config, login)
	if err != nil {
		return nil, false, err
	}

	return user, true, nil
}

// Lookup finds somebody in the directory by the name they are known by, without
// their password.
//
// For a sign-in whose identity something else has already proved - a Kerberos
// ticket - and so never offered where a password is expected: nothing here checks
// one. The search is a password sign-in's, so the same filter decides who the
// directory holds, and an entry without an address is refused as it is there.
func (l *LDAP) Lookup(ctx context.Context, login string) (*appservice.ExternalUser, bool, error) {
	l.mu.RLock()
	config := l.config
	l.mu.RUnlock()

	if !config.Enabled || config.Host == "" || login == "" {
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

	user, err := signedIn(entry, config, login)
	if err != nil {
		return nil, false, err
	}

	return user, true, nil
}

// signedIn is the account a sign-in by login reaches through entry, the same
// whether a password or a ticket proved who it was.
func signedIn(entry *ldap.Entry, config model.LDAPConfig, login string) (*appservice.ExternalUser, error) {
	email := entry.GetAttributeValue(config.EmailAttribute)
	if email == "" {
		// Refused rather than filled in with the login name, which is what this
		// used to do - and which created an account the synchronisation could
		// not account for.
		//
		// The listing is why. It keeps an entry without an address only by its
		// identifier, so where the directory gives none, a login that invented an
		// address produced an account that signed in perfectly well, could be
		// found in the listing by neither key, and was therefore read by the next
		// synchronisation as "this person left" - deleting it together with every
		// hour recorded against it, silently.
		//
		// Refusing here is the lesser harm by a wide margin: somebody cannot sign
		// in, which they will say so about, rather than losing their records
		// months later with nobody able to explain it.
		//
		// The message names the entry and the attribute because only somebody
		// who has already proved who they are reaches this line - the password
		// bound in Authenticate, or the ticket checked before Lookup - so it tells
		// the one person who can act on it exactly what to tell their
		// administrator, and tells an attacker nothing they did not already have
		// the credentials for.
		return nil, fmt.Errorf(
			"the directory entry for %q has no %s attribute, so no account can be keyed on it; "+
				"set the mail attribute under Settings to one this directory actually fills",
			login, config.EmailAttribute)
	}

	return &appservice.ExternalUser{
		ID:       stableID(entry, config.IDAttribute),
		Email:    strings.ToLower(email),
		Name:     entry.GetAttributeValue(config.NameAttribute),
		Role:     config.DefaultRole,
		Disabled: switchedOff(entry, time.Now()),
	}, nil
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

	return listed(result.Entries, config), nil
}

// listed turns the directory's answer into the entries the synchronisation
// matches on.
//
// An entry is dropped only when it carries neither a mail address nor an
// identifier. One without an address cannot become an account - there is
// nothing to key it on, so the synchronisation creates nothing for it and a
// sign-in is refused - but its identifier still says the person is there.
// Dropping it as well turned an account whose entry had lost its mail
// attribute, a mailbox removed or an attribute the bind account may no longer
// read, into a departure, and the next run deleted it with every hour recorded
// on it while its identifier was in the answer.
func listed(entries []*ldap.Entry, config model.LDAPConfig) []appservice.ExternalUser {
	users := make([]appservice.ExternalUser, 0, len(entries))

	for _, entry := range entries {
		user := appservice.ExternalUser{
			ID:    stableID(entry, config.IDAttribute),
			Email: strings.ToLower(entry.GetAttributeValue(config.EmailAttribute)),
			Name:  entry.GetAttributeValue(config.NameAttribute),
			Role:  config.DefaultRole,
		}

		if user.Email == "" && user.ID == "" {
			continue
		}

		users = append(users, user)
	}

	return users
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

// signInAttributes is what a sign-in asks of the directory for one entry: what
// becomes the account, and the two attributes by which Active Directory says the
// account is switched off - asked for here, or no entry would ever carry them.
func signInAttributes(config model.LDAPConfig) []string {
	return append(requestedAttributes(config), adAccountControl, adAccountExpires)
}

// The two attributes Active Directory keeps an account's state in. Other
// directories carry neither, and an entry without them is taken as on.
const (
	adAccountControl = "userAccountControl"
	adAccountExpires = "accountExpires"
)

// adAccountDisabled is userAccountControl's ACCOUNTDISABLE flag.
const adAccountDisabled = 0x2

// adEpochOffset is the seconds from Active Directory's epoch, 1601-01-01 UTC, to
// Unix's; accountExpires counts 100-nanosecond steps from the first.
const adEpochOffset = 11_644_473_600

// switchedOff reports whether Active Directory has switched the entry's account
// off: its disabled flag, or an expiry already past. 0 and the largest value an
// accountExpires can hold both mean it never expires.
func switchedOff(entry *ldap.Entry, now time.Time) bool {
	flags, err := strconv.ParseUint(entry.GetAttributeValue(adAccountControl), 10, 32)
	if err == nil && flags&adAccountDisabled != 0 {
		return true
	}

	expires, err := strconv.ParseInt(entry.GetAttributeValue(adAccountExpires), 10, 64)
	if err != nil || expires <= 0 || expires == math.MaxInt64 {
		return false
	}

	return now.After(time.Unix(expires/10_000_000-adEpochOffset, 0))
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
		filter, signInAttributes(config), nil))
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

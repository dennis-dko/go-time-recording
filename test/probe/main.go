// Command probe checks that a database and a directory are reachable and
// usable, using the same drivers and the same queries the application does.
//
// The point of sharing the drivers is that a pass here means something. A
// generic port check, or psql from a shell, proves a socket answers; it does
// not prove that this binary's PostgreSQL driver can authenticate with these
// settings, or that the LDAP filter returns exactly one entry, or - the one
// that actually bites - that the directory hands out the stable identifier the
// synchronisation matches accounts on.
//
//	go run ./test/probe --db postgres --dsn "postgres://gtr:...@localhost:55432/..."
//	go run ./test/probe --ldap ldap://localhost:5389 --base-dn dc=example,dc=com
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// timeout bounds every check, so an unreachable host fails in seconds with a
// readable message instead of hanging until someone gives up.
const timeout = 10 * time.Second

type options struct {
	dbDriver string
	dsn      string

	ldapURL      string
	baseDN       string
	bindDN       string
	bindPassword string
	userFilter   string
	idAttribute  string
	mail         string
}

func main() {
	opts := parseFlags()

	var failures int

	if opts.dsn != "" {
		if err := checkDatabase(opts); err != nil {
			report("database", err)

			failures++
		} else {
			fmt.Printf("  OK   database (%s) reachable and usable\n", opts.dbDriver)
		}
	}

	if opts.ldapURL != "" {
		if err := checkDirectory(opts); err != nil {
			report("directory", err)

			failures++
		}
	}

	if opts.dsn == "" && opts.ldapURL == "" {
		fmt.Fprintln(os.Stderr, "nothing to check: pass --dsn, --ldap, or both")
		flag.Usage()
		os.Exit(2)
	}

	if failures > 0 {
		os.Exit(1)
	}

	fmt.Println("\nall checks passed")
}

func parseFlags() options {
	var opts options

	flag.StringVar(&opts.dbDriver, "db", "postgres", "database driver: postgres, mysql or sqlite")
	flag.StringVar(&opts.dsn, "dsn", "", "database connection string; empty skips the database check")

	flag.StringVar(&opts.ldapURL, "ldap", "", "directory URL, e.g. ldap://localhost:5389; empty skips the check")
	flag.StringVar(&opts.baseDN, "base-dn", "dc=example,dc=com", "search base")
	flag.StringVar(&opts.bindDN, "bind-dn", "cn=admin,dc=example,dc=com", "service account; empty binds anonymously")
	flag.StringVar(&opts.bindPassword, "bind-password", "gtr-test-password", "service account password")
	flag.StringVar(&opts.userFilter, "user-filter", "(|(uid=%s)(mail=%s)(sAMAccountName=%s))",
		"login filter; %s is replaced by the login name")
	flag.StringVar(&opts.idAttribute, "id-attribute", "entryUUID",
		"the identifier that survives a rename (Active Directory: objectGUID)")
	flag.StringVar(&opts.mail, "mail-attribute", "mail", "attribute holding the mail address")

	flag.Parse()

	return opts
}

func report(what string, err error) {
	fmt.Printf("  FAIL %s: %v\n", what, err)
}

// checkDatabase opens a connection and runs a trivial query.
//
// Ping alone is not enough: several drivers report a healthy pool before the
// server has finished authenticating or has finished starting up, so a real
// round trip is what proves the connection is usable.
func checkDatabase(opts options) error {
	// The driver names are the ones the blank imports above register, which are
	// the same ones the application connects with.
	db, err := sql.Open(opts.dbDriver, opts.dsn)
	if err != nil {
		return fmt.Errorf("opening a %s connection: %w", opts.dbDriver, err)
	}

	defer func() { _ = db.Close() }()

	db.SetConnMaxLifetime(timeout)

	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("querying: %w", err)
	}

	if one != 1 {
		return fmt.Errorf("the server answered %d to SELECT 1", one)
	}

	return nil
}

// checkDirectory exercises the directory the way a sign-in and a
// synchronisation do, and reports on each step separately so a failure says
// which one broke.
func checkDirectory(opts options) error {
	conn, err := ldap.DialURL(opts.ldapURL)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", opts.ldapURL, err)
	}

	defer func() { _ = conn.Close() }()

	conn.SetTimeout(timeout)

	if opts.bindDN != "" {
		if err := conn.Bind(opts.bindDN, opts.bindPassword); err != nil {
			return fmt.Errorf("binding as %q: %w", opts.bindDN, err)
		}

		fmt.Printf("  OK   directory bind as %s\n", opts.bindDN)
	}

	entries, err := listUsers(conn, opts)
	if err != nil {
		return err
	}

	fmt.Printf("  OK   directory search under %s returned %d entr(y|ies)\n", opts.baseDN, len(entries))

	return reportIdentifiers(entries, opts)
}

func listUsers(conn *ldap.Conn, opts options) ([]*ldap.Entry, error) {
	// The login filter matches one person; listing everyone means replacing the
	// placeholder with a wildcard, exactly as the synchronisation does.
	filter := strings.ReplaceAll(opts.userFilter, "%s", "*")

	result, err := conn.Search(ldap.NewSearchRequest(
		opts.baseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		0, int(timeout.Seconds()), false,
		filter, []string{"dn", "cn", opts.mail, opts.idAttribute}, nil))
	if err != nil {
		return nil, fmt.Errorf("searching %q with %q: %w", opts.baseDN, filter, err)
	}

	if len(result.Entries) == 0 {
		return nil, errors.New(
			"the search returned nothing. Check the base DN and the filter - " +
				"the synchronisation treats an empty answer as a fault and refuses to act on it")
	}

	return result.Entries, nil
}

// reportIdentifiers is the check worth running before trusting a
// synchronisation against a real directory.
//
// Without a stable identifier the application falls back to matching on the
// mail address, and then a renamed mailbox reads as "this person left" - which
// deletes the account and every hour recorded against it.
func reportIdentifiers(entries []*ldap.Entry, opts options) error {
	var withID, withMail int

	fmt.Println()

	for _, entry := range entries {
		id := entry.GetAttributeValue(opts.idAttribute)
		if id == "" {
			// Binary values (Active Directory's objectGUID) do not survive a
			// string conversion, so fall back to the raw form.
			if raw := entry.GetRawAttributeValue(opts.idAttribute); len(raw) > 0 {
				id = fmt.Sprintf("%x", raw)
			}
		}

		mail := entry.GetAttributeValue(opts.mail)

		if id != "" {
			withID++
		}

		if mail != "" {
			withMail++
		}

		fmt.Printf("       %-46s %s=%-38s %s=%s\n",
			entry.DN, opts.idAttribute, orDash(id), opts.mail, orDash(mail))
	}

	fmt.Println()

	if withMail < len(entries) {
		fmt.Printf("  NOTE %d of %d entries have no %s and will be skipped by the sync\n",
			len(entries)-withMail, len(entries), opts.mail)
	}

	if withID == 0 {
		return fmt.Errorf(
			"no entry has %s. Accounts would be matched by mail address instead, "+
				"and renaming a mailbox would delete the account together with its "+
				"recorded hours. Active Directory uses objectGUID; pass "+
				"--id-attribute to check that instead", opts.idAttribute)
	}

	if withID < len(entries) {
		fmt.Printf("  WARN only %d of %d entries have %s; the rest fall back to mail matching\n",
			withID, len(entries), opts.idAttribute)

		return nil
	}

	fmt.Printf("  OK   every entry has %s, so a rename cannot be read as a departure\n",
		opts.idAttribute)

	return nil
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}

	return v
}

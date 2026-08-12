package rest

import (
	"strings"
	"testing"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// The restart card compares the connection this process opened against the one
// stored on disk, and this is the comparison. It is worth its own test because
// the interface now believes it: the database form reports "saved" rather than
// "applied on the next start" whenever this finds nothing, so a change this
// misses is a change nobody is told to restart for.

func TestAChangedHostIsADifferentConnection(t *testing.T) {
	running := appconfig.Datasource{
		Dialect: "postgres", Host: "db.internal", Port: "5432", Name: "gtr", User: "app",
	}

	moved := running
	moved.Host = "db2.internal"

	if connectionSummary(running) == connectionSummary(moved) {
		t.Errorf("moving the database to another host reads as no change: %q",
			connectionSummary(running))
	}
}

// Everything the form can change has to reach the summary, or the card shows
// "postgres → postgres" and calls it an explanation.
func TestEveryConnectionFieldIsVisibleInTheSummary(t *testing.T) {
	base := appconfig.Datasource{
		Dialect: "postgres", Host: "db.internal", Port: "5432",
		Name: "gtr", User: "app", SSLMode: "require",
	}

	for _, change := range []struct {
		field string
		apply func(*appconfig.Datasource)
	}{
		{"dialect", func(d *appconfig.Datasource) { d.Dialect = "mysql"; d.Port = "3306" }},
		{"host", func(d *appconfig.Datasource) { d.Host = "elsewhere" }},
		{"port", func(d *appconfig.Datasource) { d.Port = "6432" }},
		{"name", func(d *appconfig.Datasource) { d.Name = "other" }},
		{"user", func(d *appconfig.Datasource) { d.User = "reader" }},
		{"sslMode", func(d *appconfig.Datasource) { d.SSLMode = "verify-full" }},
	} {
		t.Run(change.field, func(t *testing.T) {
			changed := base
			change.apply(&changed)

			if connectionSummary(base) == connectionSummary(changed) {
				t.Errorf("a changed %s is invisible in %q", change.field, connectionSummary(base))
			}
		})
	}
}

// The password is the one thing that must not appear, which is why it is
// reported as a pending change of its own rather than through this.
func TestTheSummaryNeverCarriesThePassword(t *testing.T) {
	ds := appconfig.Datasource{
		Dialect: "postgres", Host: "db", Port: "5432",
		Name: "gtr", User: "app", Password: "hunter2",
	}

	if strings.Contains(connectionSummary(ds), ds.Password) {
		t.Errorf("the password is on screen: %q", connectionSummary(ds))
	}
}

// A connection that leaves the port empty and one that spells out the default it
// would be given are the same connection in fact, so they have to be the same
// here. Otherwise saving an unchanged form, on an installation configured by
// environment variables that never named a port, announces a restart that would
// reconnect to exactly the same server.
func TestTheDefaultPortIsNotAChange(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			unset := appconfig.Datasource{Dialect: dialect, Host: "db", Name: "gtr"}

			explicit := unset
			explicit.Port = appconfig.DefaultPortFor(dialect, "")

			if connectionSummary(unset) != connectionSummary(explicit) {
				t.Errorf("%q and %q are the same connection",
					connectionSummary(unset), connectionSummary(explicit))
			}
		})
	}
}

// A file on disk has no host, port or user, and printing empty ones would put
// "sqlite :/gtr" on the card.
func TestAFileIsDescribedByItsName(t *testing.T) {
	summary := connectionSummary(appconfig.Datasource{Dialect: "sqlite", Name: "gtr"})

	if summary != "sqlite gtr" {
		t.Errorf("a file connection reads as %q", summary)
	}
}

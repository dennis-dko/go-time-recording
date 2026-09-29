package rest

import (
	"testing"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// A kept password follows its account, and only the parts that name the account
// decide that - an empty port is the default port, as it is when connecting.
func TestAKeptPasswordFollowsItsAccountAndNothingElse(t *testing.T) {
	stored := appconfig.Datasource{
		Dialect: "postgres", Name: "gtr", Host: "db.example", Port: "5432",
		User: "gtr", Password: "secret",
	}

	for _, tc := range []struct {
		name string
		form appconfig.Datasource
		want string
	}{
		{"unchanged", stored, "secret"},
		{"the port left to its default", with(stored, func(d *appconfig.Datasource) { d.Port = "" }), "secret"},
		{"the host in capitals", with(stored, func(d *appconfig.Datasource) { d.Host = "DB.example" }), "secret"},
		{"another database there", with(stored, func(d *appconfig.Datasource) { d.Name = "other" }), "secret"},
		{"another host", with(stored, func(d *appconfig.Datasource) { d.Host = "elsewhere.example" }), ""},
		{"another port", with(stored, func(d *appconfig.Datasource) { d.Port = "6543" }), ""},
		{"another user", with(stored, func(d *appconfig.Datasource) { d.User = "someone" }), ""},
		{"another type", with(stored, func(d *appconfig.Datasource) { d.Dialect = "mysql" }), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.form.Password = ""

			if got := keptPassword(tc.form, stored); got != tc.want {
				t.Errorf("kept %q, want %q", got, tc.want)
			}
		})
	}
}

func with(d appconfig.Datasource, change func(*appconfig.Datasource)) appconfig.Datasource {
	change(&d)

	return d
}

package config

// ProbeDSNForTest exposes the connection string the pre-flight check builds.
//
// The tests live in config_test, an external package, which is what keeps them
// honest about the API this package actually offers. The one thing they cannot
// reach that way is the DSN itself - and that is precisely what has to be
// asserted, because every defect this check has had was a DSN that differed from
// the one GoFr goes on to open: a PostgreSQL port that was MySQL's, and a MySQL
// connection that was plaintext while the configured one was not.
//
// In an _test.go file, so it exists for the tests and ships in nothing.
func ProbeDSNForTest(ds Datasource) string {
	_, dsn, err := driverDSN(ds)
	if err != nil {
		return "error: " + err.Error()
	}

	return dsn
}

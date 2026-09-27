// Package harness starts the real application for a test to talk to.
//
// It is shared by the integration tests, which drive it over HTTP, and the
// browser tests, which drive it through a browser. Both want the same thing:
// the compiled binary, its own database, a free port, and a readable failure
// when it does not come up.
//
// It lives outside _test.go files so both packages can import it. That means
// it is compiled into ordinary builds, which is why it holds no test logic of
// its own - only the plumbing for starting and stopping an instance.
package harness

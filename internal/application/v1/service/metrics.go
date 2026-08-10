package service

import "context"

// The framework already measures the machinery: a histogram per HTTP request, a
// histogram per SQL query, a gauge of goroutines. None of that says whether the
// application is doing its job. A deployment can serve every request in
// milliseconds while nobody has been able to book time since the directory
// changed, and app_http_response looks the same either way.
//
// So these four measure the work rather than the plumbing, and each one is here
// because somebody would act on it:
//
//   - hours booked, which is what the installation exists to record, and whose
//     absence is the first sign that something upstream is broken;
//   - what happens to an entry afterwards, because a queue of submitted entries
//     nobody approves is invisible from any request count;
//   - refused sign-ins, which is either a directory that has stopped answering
//     or somebody working through a password list;
//   - accounts the directory synchronisation creates and deletes, which is the
//     one operation here that removes people together with their recorded hours.
//
// Deliberately absent: anything labelled with a user, an address or a project
// name. A label is a time series, and one per person is both a memory leak in
// the collector and a list of who works here, published on a port that asks for
// no password.
const (
	// MetricHoursBooked is a histogram, so it carries the total and the shape:
	// a day recorded as one eight-hour entry and one recorded as sixteen
	// half-hour entries are different installations to support.
	MetricHoursBooked = "gtr_timesheet_hours_booked"

	// MetricSignInFailures counts refused sign-ins, labelled "reason" with a
	// fixed handful of values.
	MetricSignInFailures = "gtr_signin_failures_total"

	// MetricDirectoryAccounts counts what a synchronisation did, labelled
	// "action" with "created" or "deleted".
	MetricDirectoryAccounts = "gtr_directory_accounts_total"
)

// Reasons a sign-in was refused. A closed set, because it is a label.
const (
	SignInFailureCredentials = "credentials"
	SignInFailureDirectory   = "directory"
	SignInFailureTOTP        = "totp"
)

// Recorder is the part of the framework's metrics manager this application
// records through.
//
// Declared here rather than imported so the application layer keeps not knowing
// about the framework - the manager satisfies this structurally, so there is no
// adapter to keep in step either.
type Recorder interface {
	IncrementCounter(ctx context.Context, name string, labels ...string)
	RecordHistogram(ctx context.Context, name string, value float64, labels ...string)
}

// metrics is embedded by the services that record, so each one gets the nil
// check once rather than at every call site.
//
// Nil is the ordinary state in a unit test, and a test that has to supply a
// metrics recorder to exercise a booking rule is a test about the wrong thing.
type metrics struct {
	recorder Recorder
}

func (m metrics) count(ctx context.Context, name string, labels ...string) {
	if m.recorder != nil {
		m.recorder.IncrementCounter(ctx, name, labels...)
	}
}

func (m metrics) record(ctx context.Context, name string, value float64, labels ...string) {
	if m.recorder != nil {
		m.recorder.RecordHistogram(ctx, name, value, labels...)
	}
}

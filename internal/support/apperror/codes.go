package apperror

// The reasons this application can give, named once for the whole of it.
//
// Two kinds of code live in this application and they are worth telling apart.
// Most refusals name themselves where the rule is enforced, with WithCode, and
// that is right: the rule knows why it refused, and a name written next to it can
// be as specific as the rule is - projectHasEntries, timesheetLocked,
// passkeyAlreadyRegistered. Those are declared at the point of use because that
// is the only place that knows they exist.
//
// The ones below are the other kind: the refusals that belong to no single rule.
// Every endpoint can be reached without a session, refuse for want of a
// permission, be asked for something that is not there, or fail on something
// underneath that this application does not control. Written as literals they
// were written differently in different places, and a code nobody agreed on is a
// code nothing can be built on.
//
// # Why words rather than numbers
//
// The obvious model is the numbered one - a Win32 error, an HRESULT, an errno.
// It is the right design for an interface where the code is all you get: a return
// value in a register, with a table somewhere else that says what it means.
//
// Nothing here is under that constraint. These travel in a JSON body with room
// beside them, and the property that matters is not compactness - it is that the
// code says what it means where it is read. `probeFailed` in a log line, in a
// support message, in a browser's network tab is legible on sight; 0x80070005 is
// a search. They are equally stable, equally comparable, and equally
// machine-readable; one of them is also readable.
//
// What is borrowed from the numbered systems is the part that makes them work:
// the set is closed, declared in one place, and nothing may emit a reason that is
// not in it. That is not convention here - TestEveryErrorTheAPICanGiveIsNamed
// provokes each of these against a running instance and fails if one comes back
// unnamed.
//
// # What a client can rely on
//
// A code never changes meaning. A code may be added. A code that is withdrawn is
// removed from this list, which breaks the translation test until every sentence
// for it is gone too - so nothing is left saying something the server no longer
// says.
const (
	// CodeInternal: something inside failed in a way nobody anticipated. The
	// reader gets a sentence and a reference; the detail carries what was thrown,
	// and the same reference is in the log line.
	CodeInternal = "internal"

	// CodeProbeFailed: a connection test did not get through. Almost always a
	// wrong host, a closed port, a refused password or an untrusted certificate -
	// every one of which arrives as the driver's own prose.
	CodeProbeFailed = "probeFailed"

	// CodeUnauthenticated: no session, or one that has expired. The interface
	// acts on this rather than only showing it: it is what puts the sign-in
	// screen back.
	CodeUnauthenticated = "unauthenticated"

	// CodeNotFound: asked for something that is not there. Carries the kind of
	// thing and the identifier, so the sentence can name both.
	CodeNotFound = "notFound"

	// CodeInvalidFields: named fields were rejected. The names travel beside it,
	// which is what lets the interface label them the way the form does.
	CodeInvalidFields = "invalidFields"

	// CodeRateLimited: too many requests from one caller. Carries how long to
	// wait, which is also in Retry-After.
	CodeRateLimited = "rateLimited"

	// CodeCSRFRejected: a request that did not prove it came from this
	// application's own pages. Nearly always a stale tab rather than an attack,
	// and the answer to it is to reload - which is worth saying rather than
	// leaving somebody to guess.
	CodeCSRFRejected = "csrfRejected"

	// CodeBodyTooLarge: the request carried more than the endpoint accepts. A
	// spreadsheet import is allowed far more than anything else, so this arriving
	// anywhere else is a caller sending something it has no reason to.
	CodeBodyTooLarge = "bodyTooLarge"

	// CodeMaintenance: the installation is deliberately out of service. Sent only
	// where the notice is this application's own words; an administrator who wrote
	// their own wrote it for the people who will read it, and replacing it with a
	// translation would be replacing their message with ours.
	CodeMaintenance = "maintenance"
)

// GenericCodes is every code declared above.
//
// Exported so a test can assert that what an endpoint actually answers is one of
// these, rather than something a handler invented on the way past.
var GenericCodes = []string{
	CodeInternal,
	CodeProbeFailed,
	CodeUnauthenticated,
	CodeNotFound,
	CodeInvalidFields,
	CodeRateLimited,
	CodeCSRFRejected,
	CodeBodyTooLarge,
	CodeMaintenance,
}

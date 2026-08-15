package apperror

// The generic reasons, named once for the whole application.
//
// Most refusals name themselves where the rule is enforced, with WithCode, and
// that is right: the rule knows why it refused, and a sentence written next to it
// can be specific. These are the ones that cannot be. They are what is left when
// the thing that failed was not a rule this application enforces but a database, a
// directory, a file system, a network - somebody else's library, saying something
// in English that nobody here wrote and no dictionary can cover.
//
// A code is what a reader in another language gets instead. The original wording
// still travels, as detail, because it is the only text that says what actually
// happened - it is just no longer the thing on screen.
//
// Constants rather than literals scattered about, because these are the codes an
// installation will see most and a typo in one of them is a screen that silently
// falls back to English.
const (
	// CodeInternal: something inside this application failed in a way that was
	// not anticipated. The reader gets a sentence and a reference; the detail
	// carries whatever was actually thrown, and the same reference is in the log.
	CodeInternal = "internal"

	// CodeProbeFailed: a connection test did not get through. Almost always a
	// wrong host, a closed port, a refused password or a certificate nobody
	// trusts - and every one of those arrives as the driver's own prose.
	CodeProbeFailed = "probeFailed"
)

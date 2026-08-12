package model

import "strings"

// The lengths the database columns actually are.
//
// They are here, and enforced before a write, because SQLite does not enforce
// them and the servers do. A name longer than its column is stored without
// complaint on a developer's SQLite file and rejected by PostgreSQL and MySQL
// with a driver error the caller reads as "the application is broken" - so the
// shape of the bug is a form that works everywhere it is tried and fails only
// where it matters.
//
// Each one matches a column in internal/infrastructure/persistence/migrations.
// TEXT columns have no width, and the two limits for those are chosen rather
// than derived: a description is a description, and nothing good comes of one
// large enough to slow every page that lists it.
const (
	// MaxNameLength covers users.name and projects.name, both VARCHAR(255).
	MaxNameLength = 255

	// MaxEmailLength covers users.email, VARCHAR(255).
	MaxEmailLength = 255

	// MaxRoleNameLength covers roles.name, VARCHAR(64).
	MaxRoleNameLength = 64

	// MaxTokenLabelLength covers api_tokens.name, VARCHAR(120).
	MaxTokenLabelLength = 120

	// MaxDescriptionLength bounds the TEXT columns on projects and timesheets.
	// Chosen, not derived: long enough for anything anybody types about a day's
	// work, short enough that a listing stays a listing.
	MaxDescriptionLength = 2000
)

// What the installation may call itself.
//
// The settings table stores every one of these in a TEXT column, so nothing
// below is a column width - they are the numbers the form has always claimed. It
// claimed them alone: each field carries a maxlength and the API accepted
// anything, which makes a limit a suggestion to whoever uses the screen and no
// limit at all to whoever uses the endpoint. The banner and the title are read
// by everyone who opens the sign-in page, before there is a session, so an
// unbounded one is unbounded in the one response this application hands out
// freely.
//
// A test compares each of these against the maxlength in the markup, so the two
// cannot drift apart again.
const (
	// MaxTitleLength bounds what the browser tab and the header say.
	MaxTitleLength = 80

	// MaxBannerLength bounds the announcement above the application.
	MaxBannerLength = 300

	// MaxFooterTextLength and MaxLegalNoticeLength bound the two lines at the
	// bottom of every screen.
	MaxFooterTextLength  = 200
	MaxLegalNoticeLength = 200

	// MaxCompanyNameLength bounds the name beside the footer's link.
	MaxCompanyNameLength = 120
)

// TooLong reports whether a value exceeds a column, counting the way a database
// does.
//
// Runes rather than bytes, because a VARCHAR(255) holds 255 characters and a
// name in any language that needs more than one byte per letter would otherwise
// be refused at well under its real limit - which reads as the application
// disliking the alphabet.
func TooLong(value string, limit int) bool {
	return len([]rune(strings.TrimSpace(value))) > limit
}

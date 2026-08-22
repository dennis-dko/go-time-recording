// Package service contains the application services: they orchestrate
// repositories and enforce use-case level rules, keeping both HTTP concerns
// and storage details out of the domain.
package service

import (
	"regexp"
	"strings"
)

// emailPattern is deliberately permissive. Fully validating an address by
// regex is not possible; this only rejects input that is obviously not an
// address, and delivery would be the real test.
//
// The domain may be one label. It had to have a dot in it, which reads as the
// obvious rule and is wrong on exactly the networks this application is most
// often installed on: "@local", "@intranet" and a bare host name are ordinary
// there, and the account this installation creates for itself is admin@local -
// so the screen refused to create the kind of address it had already made.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s.]+(\.[^@\s]+)?$`)

func validEmail(email string) bool {
	return emailPattern.MatchString(strings.TrimSpace(email))
}

// paginate returns the requested slice window. A non-positive page or limit
// disables paging, so callers that do not care get the full list.
func paginate[T any](items []T, page, limit int) []T {
	if limit <= 0 {
		return items
	}

	if page <= 0 {
		page = 1
	}

	start := (page - 1) * limit
	if start >= len(items) {
		return []T{}
	}

	end := min(start+limit, len(items))

	return items[start:end]
}

// Package model is the vocabulary the rest of this application is written in:
// the entities, the value types they store, and what is true about them
// wherever they are read.
//
// It imports nothing but the standard library, and that is the shape of the
// architecture rather than an accident of this package. internal/domain may
// import neither internal/infrastructure nor internal/interface, so the layer
// that holds the rules can be read and tested without a database, an HTTP
// request, or a clock it was not handed.
//
// Two decisions here are worth knowing before changing anything, because both
// were made after the obvious version went wrong.
//
// A stored date is a calendar day and not an instant - CalendarDay, midnight
// UTC, carrying no zone at all. Which day it is gets read in the reader's own
// location; what is stored knows nothing about location. The version that kept
// the zone stored 15 July as midnight in Berlin from the clock and as midnight
// UTC from the form, overtime grouped those as two days, each was measured
// against a full day's target, and the balance on screen was eight hours short
// of the truth.
//
// A project belongs to exactly one person, and that ownership is visibility.
// There is no separate sharing flag because there used to be one, and it was
// the exception that swallowed the rule. Two people working on the same thing
// have a project each, with the same name if they like, and nothing adds them
// together: there are no teams here and no view across accounts.
package model

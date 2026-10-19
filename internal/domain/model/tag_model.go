package model

import (
	"slices"
	"strings"
	"unicode"
)

// Tags returns the activities a description names - #meeting, #review - each once,
// lower-cased, in the order they first appear.
//
// An activity is a second way of sorting hours beside the project, written where
// the hours are already described rather than kept in a field of its own: nothing
// to set up before booking, and every description written so far already counts.
//
// A tag starts where a word does - at the beginning, or after something that is
// neither a letter nor a digit - so a reference like "abc#12" and the # in "C#"
// are not tags, and it runs over letters, digits, "-" and "_". One made of digits
// alone, "#123", is a ticket number rather than an activity, and is left out.
func Tags(description string) []string {
	var tags []string

	runes := []rune(description)

	for i := 0; i < len(runes); i++ {
		if runes[i] != '#' || (i > 0 && wordRune(runes[i-1])) {
			continue
		}

		end := i + 1
		for end < len(runes) && (wordRune(runes[end]) || runes[end] == '-' || runes[end] == '_') {
			end++
		}

		tag := strings.ToLower(string(runes[i+1 : end]))

		if strings.IndexFunc(tag, func(r rune) bool { return !unicode.IsDigit(r) }) >= 0 &&
			!slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}

		i = end - 1
	}

	return tags
}

// wordRune reports whether a rune belongs to a word: a letter or a digit.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

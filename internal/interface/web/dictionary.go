package web

import (
	"io/fs"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Dictionary is one language's table out of app.js - what the page says in that
// language, keyed the way the page looks it up - for the binary's other readers.
// The terminal client says a refusal, a status or a field name in the browser's
// words this way, rather than in a copy of them that would drift.
//
// Read from the embedded script, so it is always the table of the binary it runs
// in. English answers an empty table: it is the source, written into the page
// and into its fallbacks, and has no dictionary of its own.
func Dictionary(language string) map[string]string {
	script, err := fs.ReadFile(assets, "assets/app.js")
	if err != nil {
		return map[string]string{}
	}

	return dictionaryIn(string(script), language)
}

// RefusalValues is app.js's REFUSAL_VALUES, read rather than copied: for each
// refusal code, which of its values the page writes in words of its own, and
// with which of its functions - statusName, entityName or fmtDate. A reader that
// says refusals as the page does has to treat those values as the page does, or
// a German sentence says "completed" where the page says "abgeschlossen".
func RefusalValues() map[string]map[int]string {
	script, err := fs.ReadFile(assets, "assets/app.js")
	if err != nil {
		return map[string]map[int]string{}
	}

	return refusalValuesIn(string(script))
}

// dictionaryIn reads one language's table out of the script: from its line
// `  <language>: {` to the `  },` that closes it, entry by entry - a quoted key,
// a colon and a quoted value, which a long sentence writes as several literals
// joined by "+" over as many lines - with comments between them.
func dictionaryIn(script, language string) map[string]string {
	table := map[string]string{}

	_, after, found := strings.Cut(script, "const TRANSLATIONS = {")
	if !found {
		return table
	}

	_, body, found := strings.Cut(after, "\n  "+language+": {")
	if !found || strings.HasPrefix(body, "}") {
		return table
	}

	body, _, _ = strings.Cut(body, "\n  },")

	for rest := skipSpace(body); rest != ""; rest = skipSpace(rest) {
		key, after, ok := quoted(rest)
		if !ok {
			break
		}

		rest = skipSpace(after)
		if !strings.HasPrefix(rest, ":") {
			break
		}

		var value strings.Builder

		for rest = skipSpace(rest[1:]); ; {
			part, after, ok := quoted(rest)
			if !ok {
				return table
			}

			value.WriteString(part)

			if rest = skipSpace(after); !strings.HasPrefix(rest, "+") {
				break
			}

			rest = skipSpace(rest[1:])
		}

		table[key] = value.String()
		rest = strings.TrimPrefix(rest, ",")
	}

	return table
}

// skipSpace skips white space and line comments.
func skipSpace(text string) string {
	for {
		text = strings.TrimLeft(text, " \t\r\n")

		if !strings.HasPrefix(text, "//") {
			return text
		}

		_, text, _ = strings.Cut(text, "\n")
	}
}

// quoted reads the single-quoted string text starts with, and answers it with
// what follows the closing quote.
func quoted(text string) (string, string, bool) {
	var out strings.Builder

	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '\'':
			return out.String(), text[i+1:], true
		case '\\':
			if i+1 == len(text) {
				return "", "", false
			}

			i += escaped(text[i+1:], &out)
		default:
			r, size := utf8.DecodeRuneInString(text[i:])
			out.WriteRune(r)

			i += size - 1
		}
	}

	return "", "", false
}

// escaped writes what the escape after a backslash stands for, and answers how
// many bytes it took. The table uses none today, and what it might use is few: a
// quote, a backslash, a line break, a tab, and a character written by its number.
func escaped(text string, out *strings.Builder) int {
	switch text[0] {
	case 'n':
		out.WriteByte('\n')

		return 1
	case 't':
		out.WriteByte('\t')

		return 1
	case 'u', 'x':
		digits := 4
		if text[0] == 'x' {
			digits = 2
		}

		if len(text) > digits {
			if code, err := strconv.ParseUint(text[1:1+digits], 16, 32); err == nil {
				out.WriteRune(rune(code))

				return 1 + digits
			}
		}
	}

	// A quote, a backslash, or anything else a script reads as itself.
	r, size := utf8.DecodeRuneInString(text)
	out.WriteRune(r)

	return size
}

// refusalValuesIn reads REFUSAL_VALUES out of the script, a code a line:
// `  overDailyLimit: { 2: fmtDate },`.
func refusalValuesIn(script string) map[string]map[int]string {
	named := map[string]map[int]string{}

	_, after, found := strings.Cut(script, "const REFUSAL_VALUES = {")
	if !found {
		return named
	}

	body, _, _ := strings.Cut(after, "\n};")

	for _, line := range strings.Split(body, "\n") {
		code, values, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found || code == "" {
			continue
		}

		written := map[int]string{}

		for _, value := range strings.Split(strings.Trim(strings.TrimSpace(values), "{},"), ",") {
			at, function, found := strings.Cut(value, ":")
			if !found {
				continue
			}

			if index, err := strconv.Atoi(strings.TrimSpace(at)); err == nil {
				written[index] = strings.TrimSpace(function)
			}
		}

		named[code] = written
	}

	return named
}

package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
)

// language is what track says things in.
//
// Every sentence in this package is written in English and passed through tr,
// and the English is the key: german holds what it becomes. The zero value is
// English, so anything nobody gave a language speaks English, and a sentence
// nobody has translated is still said - in English, as in the web interface.
//
// What track says in the browser's words is not in german at all: a refusal, a
// project's status, a field's name and the unit an hour is written in come from
// the web interface's own dictionary, which the binary carries, so the two say
// the same thing the same way and cannot come to disagree.
type language struct {
	german bool

	// page is the web interface's German, and refusals which values of which
	// refusal it writes in words of its own.
	page     map[string]string
	refusals map[string]map[int]string
}

// inGerman is German, with the web interface's dictionary read from the binary.
func inGerman() language {
	return language{german: true, page: web.Dictionary("de"), refusals: web.RefusalValues()}
}

// chooseLanguage decides what track speaks: the --lang flag, then GTR_LANG, then
// the language the system is set to, then English.
//
// The flag and GTR_LANG are somebody asking, so a language track does not have
// is refused rather than ignored - in the language the next one down chose, and
// only where nothing above it asked for one track has: a stray GTR_LANG does not
// stand in the way of a --lang on the line. The system's setting is only a guess
// at what somebody reads - a French LANG is no reason to refuse to start - so
// anything but German there is English. system is asked last, and only where
// the environment says nothing, because LANG and its kin are how a terminal says
// it on every system but Windows, which sets none of them.
func chooseLanguage(flag string, env func(string) string, system func() string) (language, error) {
	set := ""

	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if set = strings.TrimSpace(env(name)); set != "" {
			break
		}
	}

	if set == "" {
		set = system()
	}

	chosen := languageNamed(set)

	var refused error

	for _, asked := range []struct{ from, value string }{
		{"GTR_LANG", strings.TrimSpace(env("GTR_LANG"))},
		{"--lang", flag},
	} {
		if asked.value == "" {
			continue
		}

		named, ok := languageAsked(asked.value)
		if !ok {
			refused = usageError(fmt.Sprintf(chosen.tr("%s takes de or en, not %q"), asked.from, asked.value))

			continue
		}

		chosen, refused = named, nil
	}

	return chosen, refused
}

// languageAsked reads a language somebody named: de or en, alone or as the
// start of a locale such as de_DE.UTF-8 or en-GB.
func languageAsked(name string) (language, bool) {
	switch strings.ToLower(name) {
	case "de", "en":
	default:
		if len(name) < 3 || !strings.ContainsRune("_-.", rune(name[2])) {
			return language{}, false
		}
	}

	switch strings.ToLower(name[:2]) {
	case "de":
		return inGerman(), true
	case "en":
		return language{}, true
	}

	return language{}, false
}

// languageNamed is the language a locale names, which is English unless it is
// German.
func languageNamed(locale string) language {
	named, _ := languageAsked(locale)

	return named
}

// languageFlag takes --lang out of a command line, wherever it stands - except
// as the text of -m or the day of -d, which may say anything - and answers false
// where it ends the line with nothing after it.
func languageFlag(args []string) (string, []string, bool) {
	var flag string

	rest := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-m", arg == "--message", arg == "-d", arg == "--date":
			rest = append(rest, arg)

			if i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		case arg == "--lang":
			if i+1 == len(args) {
				return "", rest, false
			}

			i++
			flag = args[i]
		case strings.HasPrefix(arg, "--lang="):
			flag = strings.TrimPrefix(arg, "--lang=")
		default:
			rest = append(rest, arg)
		}
	}

	return flag, rest, true
}

// usage is what track says about itself, in this language.
func (l language) usage() string {
	if l.german {
		return usageGerman
	}

	return usage
}

// tr is what one of track's sentences is in this language.
func (l language) tr(english string) string {
	if !l.german {
		return english
	}

	if said, ok := german[english]; ok {
		return said
	}

	return english
}

// hours writes a number of hours the way the web interface does in this
// language: 2.50 h, or 2,50 Std.
func (l language) hours(h float64) string {
	if !l.german {
		return fmt.Sprintf("%.2f h", h)
	}

	unit := l.page["unit.hours"]
	if unit == "" {
		unit = "h"
	}

	return l.number(h) + " " + unit
}

// number writes a figure to two places, the way the web interface writes one:
// 1,234.50 in English and 1.234,50 in German.
func (l language) number(v float64) string {
	point, group := ".", ","
	if l.german {
		point, group = ",", "."
	}

	whole, fraction, _ := strings.Cut(strconv.FormatFloat(math.Abs(v), 'f', 2, 64), ".")

	var grouped strings.Builder

	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteString(group)
		}

		grouped.WriteRune(digit)
	}

	sign := ""
	if v < 0 && (whole != "0" || fraction != "00") {
		sign = "-"
	}

	return sign + grouped.String() + point + fraction
}

// date writes a day that travels as YYYY-MM-DD the way the reader writes one.
// English keeps the form it travels in, which is how track has always written it.
func (l language) date(day string) string {
	at, err := time.Parse(time.DateOnly, day)
	if err != nil || !l.german {
		return day
	}

	return at.Format("02.01.2006")
}

// weekday writes a day of the week and its date: Monday, 2 January.
func (l language) weekday(at time.Time) string {
	if !l.german {
		return at.Format("Monday, 2 January")
	}

	return fmt.Sprintf("%s, %d. %s", germanWeekdays[at.Weekday()], at.Day(), germanMonths[at.Month()-1])
}

// refusal reads what the installation said when it refused a request.
//
// Not signed in is said in track's own words, because the installation's are
// about a session and what is wrong here is the token. Anything else is the
// installation's sentence - in English as it wrote it, and in German as the web
// interface says it, from the code it sent and the values that go into it.
func (l language) refusal(status int, raw []byte) error {
	if status == http.StatusUnauthorized {
		return refusal(l.tr("the installation does not know this token - make one under My account, API tokens, and put it in GTR_TOKEN"))
	}

	var body struct {
		Error said `json:"error"`
	}

	_ = json.Unmarshal(raw, &body)

	if l.german {
		if sentence := l.inPageWords(body.Error); sentence != "" {
			return refusal(sentence)
		}
	}

	if body.Error.Message != "" {
		return refusal(body.Error.Message)
	}

	return refusal(fmt.Sprintf(l.tr("the installation answered %d %s"), status, http.StatusText(status)))
}

// said is a refusal as the installation sends it.
type said struct {
	Message string   `json:"message"`
	Code    string   `json:"code"`
	Values  []any    `json:"values"`
	Param   []string `json:"param"`
}

// inPageWords is a refusal as the web interface says it, or nothing where its
// dictionary has no sentence for it. The same order as its describeRefusal: the
// fields named first, because which ones is more use than any sentence.
func (l language) inPageWords(refused said) string {
	if refused.Code == "maintenance" {
		return l.page["err.maintenance"]
	}

	if len(refused.Param) > 0 && l.page["err.invalidFields"] != "" {
		named := make([]string, len(refused.Param))

		for i, field := range refused.Param {
			named[i] = field
			if label := l.page["field."+field]; label != "" {
				named[i] = label
			}
		}

		return l.page["err.invalidFields"] + ": " + strings.Join(named, ", ")
	}

	sentence := l.page["err."+refused.Code]
	if refused.Code == "" || sentence == "" {
		return ""
	}

	return l.fillIn(sentence, l.refusalValues(refused.Code, refused.Values))
}

// refusalValues writes the values of a refusal the way the web interface's
// REFUSAL_VALUES says: a status and a kind of record in its words, a day the
// way the reader writes one.
func (l language) refusalValues(code string, values []any) []any {
	named, writers := l.refusals[code], l.valueWriters()
	written := make([]any, len(values))

	for i, value := range values {
		written[i] = value

		if text, isText := value.(string); isText && writers[named[i]] != nil {
			written[i] = writers[named[i]](text)
		}
	}

	return written
}

// valueWriters are the ways REFUSAL_VALUES writes a value, by the name of the
// page's function for each. A test fails on a way REFUSAL_VALUES names that this
// does not have, which would otherwise reach a sentence as the word it travels in.
func (l language) valueWriters() map[string]func(string) string {
	return map[string]func(string) string{
		"statusName": func(status string) string { return l.pageWord("status.", status) },
		"entityName": func(entity string) string { return l.pageWord("entity.", entity) },
		"fmtDate":    l.date,
	}
}

// pageWord is one of the web interface's words, or the word itself where it has
// none.
func (l language) pageWord(prefix, word string) string {
	if said := l.page[prefix+word]; said != "" {
		return said
	}

	return word
}

// fillIn puts the values back into a sentence where it says {0}, {1} ..., as the
// web interface's fillIn does: a whole number as it is, a fraction to two places,
// and a place with no value left as it stands.
func (l language) fillIn(sentence string, values []any) string {
	var out strings.Builder

	for rest := sentence; rest != ""; {
		before, after, found := strings.Cut(rest, "{")
		out.WriteString(before)

		if !found {
			break
		}

		digits, tail, closed := strings.Cut(after, "}")

		at, err := strconv.Atoi(digits)
		if !closed || err != nil || at < 0 || at >= len(values) || values[at] == nil {
			out.WriteString("{")

			rest = after

			continue
		}

		out.WriteString(l.value(values[at]))

		rest = tail
	}

	return out.String()
}

// value is one value of a refusal as a sentence writes it.
func (l language) value(v any) string {
	number, isNumber := v.(float64)
	if !isNumber {
		return fmt.Sprint(v)
	}

	if number == math.Trunc(number) {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}

	return l.number(number)
}

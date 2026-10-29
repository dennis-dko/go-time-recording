package cli

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/interface/web"
	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// language is what the client says things in.
//
// Its sentences are keyed track.* in the dictionaries every part of the
// application reads, with the English written where they are said, as the web
// interface does it. What the web interface says as well - a refusal, a status,
// a field's name, the unit of an hour - is looked up under the web interface's
// own key, so the terminal and the browser say one thing one way. The zero
// value is English, so anything nobody gave a language speaks English, and a
// sentence a dictionary lacks is still said, in English.
type language struct {
	words i18n.Translator

	// refusals names which values of which refusal the web interface writes in
	// words of its own.
	refusals map[string]map[int]string
}

// speaking is the language a code names, English for one there are no words
// for.
func speaking(code string) language {
	if code == i18n.English || !slices.Contains(i18n.Languages(), code) {
		return language{}
	}

	return language{words: i18n.For(code), refusals: web.RefusalValues()}
}

// code is the language's code: en, de ...
func (l language) code() string {
	if code := l.words.Language(); code != "" {
		return code
	}

	return i18n.English
}

// spoken is the languages there are words for, as somebody types them.
func spoken() string { return strings.Join(i18n.Languages(), ", ") }

// chooseLanguage decides what the client speaks, as far as it can be told
// without asking the installation: the --lang flag, then GTR_LANG, then the
// language the system is set to, then English. explicit says whether somebody
// asked, which is what decides whether the account's own language is asked for
// next.
//
// The flag and GTR_LANG are somebody asking, so a language the client does not
// have is refused rather than ignored - in the language the next one down
// chose, and only where nothing above it asked for one the client has: a stray
// GTR_LANG does not stand in the way of a --lang on the line. The system's
// setting is only a guess at what somebody reads - a French LANG is no reason
// to refuse to start - so a language there is no dictionary for is English.
// system is asked last, and only where the environment says nothing, because
// LANG and its kin are how a terminal says it on every system but Windows,
// which sets none of them.
func chooseLanguage(flag string, env func(string) string, system func() string) (chosen language, explicit bool, refused error) {
	set := ""

	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if set = strings.TrimSpace(env(name)); set != "" {
			break
		}
	}

	if set == "" {
		set = system()
	}

	chosen = languageNamed(set)

	for _, asked := range []struct{ from, value string }{
		{"GTR_LANG", strings.TrimSpace(env("GTR_LANG"))},
		{"--lang", flag},
	} {
		if asked.value == "" {
			continue
		}

		named, ok := languageAsked(asked.value)
		if !ok {
			refused = usageError(chosen.text("track.lang.unknown", `{0} takes {1}, not "{2}"`,
				asked.from, spoken(), asked.value))

			continue
		}

		chosen, explicit, refused = named, true, nil
	}

	return chosen, explicit, refused
}

// languageAsked reads a language somebody named: its code alone, or as the
// start of a locale such as de_DE.UTF-8 or en-GB - and false for one there are
// no words for.
func languageAsked(name string) (language, bool) {
	code, _, _ := strings.Cut(strings.ToLower(name), "_")
	code, _, _ = strings.Cut(code, "-")
	code, _, _ = strings.Cut(code, ".")

	if !slices.Contains(i18n.Languages(), code) {
		return language{}, false
	}

	return speaking(code), true
}

// languageNamed is the language a locale names, which is English unless there
// are words for it.
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

// text is what a key says in this language, or english where there are no
// words for it, with values put in where it says {0}, {1} ...
func (l language) text(key, english string, values ...string) string {
	return l.words.Text(key, english, values...)
}

// usage is what the client says about itself, in this language.
func (l language) usage() string {
	return l.text("track.usage", usage)
}

// hours writes a number of hours the way the web interface does in this
// language: 2.50 h, or 2,50 Std.
func (l language) hours(h float64) string {
	return l.number(h) + " " + l.text("unit.hours", "h")
}

// number writes a figure to two places, the way the web interface writes one:
// 1,234.50 in English and 1.234,50 in German.
func (l language) number(v float64) string { return l.words.Number(v) }

// date writes a day that travels as YYYY-MM-DD the way the reader writes one.
// English keeps the form it travels in, which is how the client has always
// written it; a dictionary names its own as a Go layout.
func (l language) date(day string) string {
	at, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}

	return l.words.Day(at)
}

// weekday writes a day of the week and its date: Tuesday, 27 October. The
// names are a dictionary's own, Sunday first and January first, where it has
// them.
func (l language) weekday(at time.Time) string {
	days := strings.Fields(l.text("track.weekdays", ""))
	months := strings.Fields(l.text("track.months", ""))

	day, month := at.Weekday().String(), at.Month().String()

	if len(days) == 7 {
		day = days[at.Weekday()]
	}

	if len(months) == 12 {
		month = months[at.Month()-1]
	}

	return l.text("track.weekday", "{0}, {1} {2}", day, strconv.Itoa(at.Day()), month)
}

// refusal reads what the installation said when it refused a request.
//
// Not signed in is said in the client's own words, because the installation's are
// about a session and what is wrong here is the token. Anything else is the
// installation's sentence - in English as it wrote it, and in another language
// as the web interface says it, from the code it sent and the values that go
// into it.
func (l language) refusal(status int, raw []byte) error {
	if status == http.StatusUnauthorized {
		return refusal(l.text("track.tokenUnknown",
			"the installation does not know this token - make one under My account, API tokens, and put it in GTR_TOKEN"))
	}

	var body struct {
		Error said `json:"error"`
	}

	_ = json.Unmarshal(raw, &body)

	if sentence := l.inPageWords(body.Error); sentence != "" {
		return refusal(sentence)
	}

	if body.Error.Message != "" {
		return refusal(body.Error.Message)
	}

	return refusal(l.text("track.answered", "the installation answered {0} {1}",
		strconv.Itoa(status), http.StatusText(status)))
}

// said is a refusal as the installation sends it.
type said struct {
	Message string   `json:"message"`
	Code    string   `json:"code"`
	Values  []any    `json:"values"`
	Param   []string `json:"param"`
}

// inPageWords is a refusal as the web interface says it, or nothing where its
// dictionary has no sentence for it - which in English is always, the web
// interface's English being the fallbacks in its own script. The same order as
// its describeRefusal: the fields named first, because which ones is more use
// than any sentence.
func (l language) inPageWords(refused said) string {
	if refused.Code == "maintenance" {
		sentence, _ := l.words.Lookup("err.maintenance")

		return sentence
	}

	if fields, ok := l.words.Lookup("err.invalidFields"); ok && len(refused.Param) > 0 {
		named := make([]string, len(refused.Param))

		for i, field := range refused.Param {
			named[i] = l.pageWord("field.", field)
		}

		return fields + ": " + strings.Join(named, ", ")
	}

	sentence, ok := l.words.Lookup("err." + refused.Code)
	if refused.Code == "" || !ok {
		return ""
	}

	return i18n.Fill(sentence, l.refusalValues(refused.Code, refused.Values)...)
}

// refusalValues writes the values of a refusal the way the web interface's
// REFUSAL_VALUES and fillIn say: a status and a kind of record in its words, a
// day the way the reader writes one, a whole number as it is and a fraction to
// two places.
func (l language) refusalValues(code string, values []any) []string {
	named, writers := l.refusals[code], l.valueWriters()
	written := make([]string, len(values))

	for i, value := range values {
		switch v := value.(type) {
		case string:
			written[i] = v
			if writers[named[i]] != nil {
				written[i] = writers[named[i]](v)
			}
		case float64:
			written[i] = l.number(v)
			if v == math.Trunc(v) {
				written[i] = strconv.FormatFloat(v, 'f', -1, 64)
			}
		case nil:
			written[i] = ""
		default:
			encoded, _ := json.Marshal(v)
			written[i] = string(encoded)
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
	if said, ok := l.words.Lookup(prefix + word); ok {
		return said
	}

	return word
}

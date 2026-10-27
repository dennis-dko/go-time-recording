package cli

// german is what track says in German, keyed by what it says in English.
//
// The third dictionary in this repository, beside app.js's and the installer's,
// and like the installer's not a copy of the page's: these are track's own
// sentences, which the page never says. What the page does say - a refusal, a
// status, a field, the unit of an hour - track takes from the page's dictionary
// instead (see language), so it is said once, in one place.
//
// The tests read every tr call in this package and hold this table to them: a
// sentence with no German here, an entry nothing says any more, and an entry
// whose formatting verbs do not match its key's all fail.
var german = map[string]string{
	// Starting.
	"--lang needs de or en after it": "--lang braucht de oder en dahinter",
	"%s takes de or en, not %q":      "%s nimmt de oder en, nicht %q",
	"track needs GTR_URL, the installation's address, and GTR_TOKEN, a personal token made under My account.": "track braucht GTR_URL, die Adresse der Installation, und GTR_TOKEN, " +
		"ein persönliches Token, angelegt unter Mein Konto.",
	"track does not know %q; track help says what it does": "track kennt %q nicht; track help sagt, was es kann",

	// Reading a command line.
	"-m needs the text after it":                          "-m braucht den Text dahinter",
	"-d needs a day after it, as YYYY-MM-DD":              "-d braucht einen Tag dahinter, als JJJJ-MM-TT",
	"status takes nothing after it":                       "status nimmt nichts dahinter",
	"today takes nothing after it":                        "today nimmt nichts dahinter",
	"book needs the hours first: track book 1.5 Website":  "book braucht zuerst die Stunden: track book 1,5 Website",
	"%q is not a number of hours: write 1.5, 1,5 or 1:30": "%q ist keine Stundenzahl: schreib 1,5, 1.5 oder 1:30",
	"%q is not a day: write it as YYYY-MM-DD":             "%q ist kein Tag: schreib ihn als JJJJ-MM-TT",
	"no active project of yours is called %q":             "keins deiner aktiven Projekte heißt %q",
	"%q could be any of %s; say which":                    "%q passt auf mehrere: %s; sag, welches",
	"stop takes nothing after it: the stopwatch already knows what it is on": "stop nimmt nichts dahinter: " +
		"Die Stoppuhr weiß schon, worauf sie läuft",

	// What a command says.
	"The stopwatch is not running.":       "Die Stoppuhr läuft nicht.",
	"Running since %s on %s - %s so far.": "Läuft seit %s auf %s – bisher %s.",
	"Started at %s on %s.":                "Um %s auf %s gestartet.",
	"Booked %s on %s.":                    "%s auf %s gebucht.",
	"Booked %s on %s; running on %s now.": "%s auf %s gebucht; läuft jetzt auf %s.",
	"Booked %s on %s for %s.":             "%s auf %s für den %s gebucht.",
	"Booked today: %s.":                   "Heute gebucht: %s.",
	"Nothing booked today.":               "Heute ist nichts gebucht.",
	"in total":                            "insgesamt",
	"no project":                          "kein Projekt",
	"project %d":                          "Projekt %d",

	// Talking to the installation.
	"%q is not an address this can ask: %w": "%q ist keine Adresse, an die sich eine Anfrage richten lässt: %w",
	"could not reach %s: %w":                "%s ist nicht erreichbar: %w",
	"reading the answer from %s: %w":        "die Antwort von %s ließ sich nicht lesen: %w",
	"%s answered with something that is not this application's": "%s hat mit etwas geantwortet, " +
		"das nicht von dieser Anwendung stammt",
	"the installation answered %d %s": "die Installation antwortete %d %s",
	"the installation does not know this token - make one under My account, API tokens, and put it in GTR_TOKEN": "die Installation kennt dieses Token nicht – " +
		"leg unter Mein Konto, API-Tokens eines an und trag es in GTR_TOKEN ein",

	// The screen.
	"the screen could not be opened: %w": "die Ansicht ließ sich nicht öffnen: %w",
	"Reading %s …":                       "%s wird gelesen …",
	"Nothing read from %s yet.":          "Von %s ist noch nichts gelesen.",
	"since %s":                           "seit %s",
	"Today":                              "Heute",
	"Nothing booked yet.":                "Noch nichts gebucht.",
	"Sending …":                          "Wird gesendet …",
	"start":                              "starten",
	"switch":                             "wechseln",
	"stop":                               "stoppen",
	"book":                               "buchen",
	"refresh":                            "neu lesen",
	"cancel":                             "abbrechen",
	"quit":                               "beenden",

	// The forms.
	"Start the stopwatch":                  "Stoppuhr starten",
	"Switch to another project":            "Auf ein anderes Projekt wechseln",
	"What has run so far is booked on %s.": "Was bisher gelaufen ist, wird auf %s gebucht.",
	"Stop the stopwatch":                   "Stoppuhr anhalten",
	"Book %s on %s?":                       "%s auf %s buchen?",
	"Stop and book":                        "Stoppen und buchen",
	"Keep it running":                      "Weiterlaufen lassen",
	"Book time by hand":                    "Zeit von Hand buchen",
	"Hours":                                "Stunden",
	"1.5, 1,5 or 1:30":                     "1,5, 1.5 oder 1:30",
	"Day":                                  "Tag",
	"YYYY-MM-DD":                           "JJJJ-MM-TT",
	"Project":                              "Projekt",
	"No project":                           "Kein Projekt",
	"Description":                          "Beschreibung",
	"optional":                             "optional",

	// What huh's keys do, in the line under a form.
	"complete":     "vervollständigen",
	"back":         "zurück",
	"next":         "weiter",
	"submit":       "absenden",
	"select":       "auswählen",
	"up":           "hoch",
	"down":         "runter",
	"left":         "links",
	"right":        "rechts",
	"filter":       "filtern",
	"set filter":   "Filter setzen",
	"clear filter": "Filter leeren",
	"½ page up":    "½ Seite hoch",
	"½ page down":  "½ Seite runter",
	"go to start":  "zum Anfang",
	"go to end":    "zum Ende",
	"toggle":       "umschalten",
	"Yes":          "Ja",
	"No":           "Nein",
}

// usageGerman is what track says about itself, in German.
const usageGerman = `go-time-recording track - Zeit im Terminal buchen

  track                              im Terminal eine Ansicht: die laufende
                                     Stoppuhr, die Einträge von heute und ein
                                     Formular für jeden der Befehle unten
  track status                       die Stoppuhr, und was heute zusammenkommt
  track start [Projekt] [-m Text]    die Stoppuhr starten, mit Projekt oder ohne
  track stop                         sie anhalten und buchen, was sie gemessen hat
  track switch [Projekt] [-m Text]   buchen, was sie gemessen hat, und auf einem
                                     anderen weiterlaufen lassen
  track book <Stunden> [Projekt] [-m Text] [-d JJJJ-MM-TT]
                                     Stunden von Hand buchen: 1,5, 1.5 oder 1:30
  track today                        die Einträge von heute und ihre Summe

  --lang de|en                       die Sprache von track, egal wo sie in der
                                     Zeile steht; GTR_LANG merkt sie sich, und
                                     sonst gilt die des Systems

Die Installation steht in GTR_URL, zum Beispiel https://time.example.com, und das
Konto ist das des persönlichen Tokens in GTR_TOKEN - angelegt unter Mein Konto,
API-Tokens. Ein Projekt wird genannt, wie es heißt, oder mit dem Anfang seines
Namens.
`

// germanWeekdays and germanMonths name the days and months in German, in Go's
// order: Sunday first, January first.
var (
	germanWeekdays = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	germanMonths   = [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August",
		"September", "Oktober", "November", "Dezember"}
)

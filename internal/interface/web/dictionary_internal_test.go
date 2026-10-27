package web

import "testing"

// The reading takes what a script may write: a value joined from several
// literals over several lines, which the table does, and an escaped quote, a
// backslash or a character by its number, which it does not today - a comment
// between entries, and the next language's entries left to it.
func TestADictionaryIsReadAsAScriptWritesIt(t *testing.T) {
	script := "const TRANSLATIONS = {\n" +
		"  en: {},\n" +
		"  de: {\n" +
		"    // a comment between entries\n" +
		"    'a': 'it\\'s',\n" +
		"    'b': 'one ' + 'two'\n" +
		"      + ' three',\n" +
		"    'c': '\\u00e4 \\x41 \\n',\n" +
		"    'd': 'a \\\\ backslash',\n" +
		"  },\n" +
		"  fr: {\n" +
		"    'a': 'le français',\n" +
		"  },\n" +
		"};\n"

	read := dictionaryIn(script, "de")

	for key, want := range map[string]string{"a": "it's", "b": "one two three", "c": "ä A \n", "d": `a \ backslash`} {
		if read[key] != want {
			t.Errorf("%s is read as %q, want %q", key, read[key], want)
		}
	}

	if len(read) != 4 {
		t.Errorf("read %d entries, want the four of de: %v", len(read), read)
	}

	if english := dictionaryIn(script, "en"); len(english) != 0 {
		t.Errorf("the empty English table is read as %v", english)
	}
}

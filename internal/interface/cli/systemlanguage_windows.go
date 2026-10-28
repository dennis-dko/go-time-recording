package cli

import "golang.org/x/sys/windows"

// systemLanguage is the language Windows shows its own screens in, such as
// de-DE. Windows sets no LANG, so without asking it a German Windows would get
// an English client.
func systemLanguage() string {
	languages, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(languages) == 0 {
		return ""
	}

	return languages[0]
}

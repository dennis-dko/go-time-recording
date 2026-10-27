//go:build !windows

package cli

// systemLanguage has nothing to add here: everywhere but Windows, the language a
// terminal is set to is LANG and its kin, which chooseLanguage reads first.
func systemLanguage() string { return "" }

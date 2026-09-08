package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// A doc comment that opens with somebody else's name is the one comment fault
// that reads correctly in the file.
//
// ST1020, ST1021 and ST1022 are enabled here - `.golangci.yml` sets
// `checks: [all, -ST1003]`, which is deliberately wider than golangci-lint's
// default - and CLAUDE.md says why: they compare a comment's first word against
// the declaration the *toolchain* attached it to, which is how TabName's comment
// was found sitting above TitleIn.
//
// They only ever looked at half of this tree. All three are exported-only, and
// inside a grouped `const (...)` block the group's own comment satisfies them, so
// the members go unexamined. Measured with a sibling repository's doccheck: 29
// declarations here open with a word that is not their name, none of them
// reported by golangci-lint, and two were real - a method still described by the
// name it had before a rename, and a pair of comments merged into one block so
// that one type opened with a function's first sentence and the function opened
// mid-thought.
//
// What this case does *not* do is require every doc comment to start with its
// name. Fifteen of those 29 are ordinary prose in grouped constant blocks ("A
// session shorter than this would sign people out while they work"), and a rule
// that reddened the gate over those would be a rewrite of a style this tree has
// chosen, not a check. The narrower question is the one that has teeth: does the
// comment open with something that looks like *another declaration's name*?
// Those are the two shapes that hurt - a name left behind by a rename, and a
// comment that has drifted onto the wrong thing.
func TestNoDocCommentOpensWithAnotherDeclarationsName(t *testing.T) {
	for _, dir := range []string{"internal", "cmd"} {
		for _, path := range goFilesUnder(t, filepath.Join("..", dir)) {
			checkDocComments(t, path)
		}
	}
}

// A doc comment opens with the name of what it documents.
//
// CLAUDE.md states this and calls it "not a style preference here, it is the only
// thing standing between a reader and somebody else's description". Nothing
// enforced it below the exported surface, and the tree had drifted: twenty-two
// declarations opened with a sentence instead of a name, in a const block where
// the members either side of them did it correctly.
//
// Three shapes are not violations and are recognised rather than listed. A
// leading article - "A Sealer encrypts the values..." - is idiomatic Go and what
// ST1021 already accepts. A spec declaring two names at once, like
// `HeaderWidth, HeaderHeight = 440, 80`, is documented by one comment naming
// both. And a comment introducing a group is a sentence about the group, not
// about its first member; that is recognised by the member below it having no
// comment of its own, which is what a group looks like and what a documented run
// of constants does not.
func TestADocCommentOpensWithTheNameItDocuments(t *testing.T) {
	for _, dir := range []string{"internal", "cmd"} {
		for _, path := range goFilesUnder(t, filepath.Join("..", dir)) {
			checkDocOpenings(t, path)
		}
	}
}

// checkDocOpenings reports declarations whose doc comment does not open with
// their own name.
func checkDocOpenings(t *testing.T, path string) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}

	for _, documented := range namedDecls(file) {
		if opensWith(documented.doc, documented.name) {
			continue
		}

		t.Errorf("%s:%d: the doc comment on %s opens with %q rather than with its "+
			"own name. `go doc` prints the two together, so a reader meets the "+
			"sentence before they meet what it is about",
			filepath.ToSlash(path), fset.Position(documented.pos).Line,
			documented.name, firstWord(documented.doc))
	}
}

// namedDecls returns the declarations whose comment is about one named thing:
// every documented function and type, and the members of a const or var block
// that carry their own comment and declare a single name.
func namedDecls(file *ast.File) []documented {
	var out []documented

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil && !skipName(d.Name.Name) {
				out = append(out, documented{d.Name.Name, d.Doc.Text(), d.Pos()})
			}

		case *ast.GenDecl:
			// An unparenthesised declaration carries its comment on the block
			// rather than on the spec, so `var x = 1` above a doc comment reaches
			// this with s.Doc nil and d.Doc set. Missed once: the case passed over
			// a deliberately broken comment on scheduleBounds and said nothing.
			blockDoc := ""
			if d.Doc != nil && len(d.Specs) == 1 {
				blockDoc = d.Doc.Text()
			}

			for i, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					doc := blockDoc
					if s.Doc != nil {
						doc = s.Doc.Text()
					}

					if doc != "" && !skipName(s.Name.Name) {
						out = append(out, documented{s.Name.Name, doc, s.Pos()})
					}

				case *ast.ValueSpec:
					doc := blockDoc
					if s.Doc != nil {
						doc = s.Doc.Text()
					}

					if doc == "" || len(s.Names) != 1 || skipName(s.Names[0].Name) {
						continue
					}

					if introducesGroup(d, i) {
						continue
					}

					out = append(out, documented{s.Names[0].Name, doc, s.Pos()})
				}
			}
		}
	}

	return out
}

// introducesGroup reports whether the comment on the spec at index belongs to
// the run of declarations that follows rather than to that spec alone, which is
// what an undocumented member immediately below it means.
func introducesGroup(block *ast.GenDecl, index int) bool {
	if index+1 >= len(block.Specs) {
		return false
	}

	next, ok := block.Specs[index+1].(*ast.ValueSpec)

	return ok && next.Doc == nil
}

// opensWith reports whether a doc comment starts with the name, allowing the
// article Go's own convention allows in front of it.
func opensWith(doc, name string) bool {
	fields := strings.Fields(doc)
	if len(fields) == 0 {
		return true
	}

	if strings.TrimRight(fields[0], ".,:;") == name {
		return true
	}

	switch fields[0] {
	case "A", "An", "The":
		return len(fields) > 1 && strings.TrimRight(fields[1], ".,:;") == name
	}

	return false
}

// checkDocComments reads one file and reports every doc comment that opens with
// a name other than the one it is attached to.
func checkDocComments(t *testing.T, path string) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}

	declared := declaredNames(file)

	for _, documented := range documentedDecls(file) {
		first := firstWord(documented.doc)
		if first == "" || first == documented.name {
			continue
		}

		if !looksLikeAnotherName(first, declared) {
			continue
		}

		t.Errorf("%s:%d: the doc comment on %s opens with %q. Either the "+
			"declaration was renamed and the comment kept the old name, or the "+
			"comment belongs to something else and has drifted onto this one - "+
			"which reads correctly here and renders wrongly in `go doc`",
			filepath.ToSlash(path), fset.Position(documented.pos).Line,
			documented.name, first)
	}
}

// documented is one declaration that carries a doc comment.
type documented struct {
	name string
	doc  string
	pos  token.Pos
}

// documentedDecls returns every declaration in the file that has a doc comment,
// including the members of a grouped const or var block - which are exactly the
// ones the staticcheck rules stop at.
func documentedDecls(file *ast.File) []documented {
	var out []documented

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil && !skipName(d.Name.Name) {
				out = append(out, documented{d.Name.Name, d.Doc.Text(), d.Pos()})
			}

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				out = append(out, documentedSpec(d, spec)...)
			}
		}
	}

	return out
}

// documentedSpec returns the documented names a single spec contributes.
//
// A spec's own comment wins over the block's, and a spec with none falls back to
// the block's comment only when the block declares exactly one thing - otherwise
// a group comment would be compared against every member in turn and report all
// of them for a sentence written about the group.
func documentedSpec(block *ast.GenDecl, spec ast.Spec) []documented {
	var out []documented

	group := ""
	if block.Doc != nil && len(block.Specs) == 1 {
		group = block.Doc.Text()
	}

	switch s := spec.(type) {
	case *ast.TypeSpec:
		doc := group
		if s.Doc != nil {
			doc = s.Doc.Text()
		}

		if doc != "" && !skipName(s.Name.Name) {
			out = append(out, documented{s.Name.Name, doc, s.Pos()})
		}

	case *ast.ValueSpec:
		doc := group
		if s.Doc != nil {
			doc = s.Doc.Text()
		}

		if doc == "" {
			return nil
		}

		// Only the first name, because `a, b = 1, 2` has one comment for both and
		// the convention names the first.
		if len(s.Names) > 0 && !skipName(s.Names[0].Name) {
			out = append(out, documented{s.Names[0].Name, doc, s.Pos()})
		}
	}

	return out
}

// declaredNames is every name declared at the top level of the file, so that a
// comment opening with one of them can be recognised even when the word is an
// ordinary English one - `Own` reads as prose until you notice the file declares
// something called Own two lines further down.
func declaredNames(file *ast.File) map[string]bool {
	out := map[string]bool{}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out[d.Name.Name] = true

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					out[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, name := range s.Names {
						out[name.Name] = true
					}
				}
			}
		}
	}

	return out
}

// skipName reports whether a declaration is one the convention cannot apply to.
// The blank identifier carries the compile-time interface assertions, whose
// comments describe the assertion rather than a name that does not exist.
func skipName(name string) bool {
	return name == "_" || name == "init" || name == "main"
}

// firstWord returns the first word of a doc comment, without the punctuation a
// sentence puts on it.
func firstWord(doc string) string {
	fields := strings.Fields(doc)
	if len(fields) == 0 {
		return ""
	}

	return strings.TrimRight(fields[0], ".,:;")
}

// looksLikeAnotherName reports whether a comment's opening word is a declaration
// name rather than the start of a sentence.
//
// Two ways to be one, and both are needed. A word this file declares is one
// however ordinary it reads. And a compound identifier is one wherever it came
// from - an inner capital with a lowercase letter somewhere, which is what
// separates requireAdministrator and HeaderWidth from prose, from an acronym
// like HTTP or SQL, and from an ordinary capitalised first word.
func looksLikeAnotherName(word string, declared map[string]bool) bool {
	if declared[word] {
		return true
	}

	var inner, lower bool

	for i, r := range word {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}

		if i > 0 && unicode.IsUpper(r) {
			inner = true
		}

		if unicode.IsLower(r) {
			lower = true
		}
	}

	return inner && lower
}

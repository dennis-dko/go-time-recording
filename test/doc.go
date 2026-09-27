// Package test holds the checks that belong to no single package.
//
// It began as checks on the test environment itself - the compose files, and
// whether the services they start are the ones a deployment runs - and it now
// also holds the rules that span the tree: the dependency direction between the
// layers, the naming rule, that CLAUDE.md's line references still point at what
// they name, and that every table with a foreign key to an account is emptied
// when the account is erased. Each of those was previously a claim in a markdown
// file, re-measured by hand at each audit and true until somebody forgot.
//
// They live together because none of them is about one package: they read across
// several, or they read a document beside the code. A check for one package's own
// invariant belongs in that package.
//
// Nothing here needs a build tag, so it runs in the ordinary unit job rather than
// only where Docker exists - reading YAML or Go source needs no daemon, and a
// check that only runs in the slow job is a check nobody waits for.
package test

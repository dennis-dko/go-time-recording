// Command nextversion prints the version a merge to main is released as.
//
// The release workflow asks it with the newest version tag, or with nothing when
// there is none yet. A tag pushed by hand still releases exactly what it names;
// this decides only the merge path.
package main

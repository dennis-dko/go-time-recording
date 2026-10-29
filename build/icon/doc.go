// Command icon draws the shipped mark and writes it as build/icon.ico.
//
// The .exe gets its icon from that file, by way of goversioninfo and
// build/windows-resource.sh. It is a binary in the tree, which is a copy of a
// picture that also exists as internal/interface/web/asset/favicon.svg - and a
// copy nobody can regenerate is a copy that silently keeps the old mark forever.
// That is exactly what happened to the tab icon: it was cached at an address
// that could not change, and went on showing a picture nobody had chosen. This
// is the answer for the Windows side of the same problem.
//
// The mark is drawn by imaging.Mark, from favicon.svg's geometry in the same
// 32-unit box - the same drawing the server uses for an installed app's icons,
// so the two cannot come to show different pictures. Changing the SVG means
// changing that function.
//
//	go run ./build/icon > build/icon.ico
//
// Four sizes, because Windows asks for four: 16 in the Explorer's detail view,
// 32 in the list and on the taskbar, 48 medium, 256 in the file's properties and
// wherever a preview is large. Each is drawn at its own size rather than scaled
// from one big one - a 256 shrunk to 16 loses the corners of the tile.
package main

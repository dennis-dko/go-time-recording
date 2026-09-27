// Package imaging makes the sizes of a logo an installation is shown in.
//
// A logo is uploaded once, for a header, and then drawn in three places that
// want three very different things: a mark beside the title, a banner over a
// sign-in card, and sixteen pixels in a browser tab. Handing the same few
// thousand pixels to all three is what this exists to stop - most visibly in the
// tab, where a browser given an image that size makes its own decision about
// whether to use it at all.
package imaging

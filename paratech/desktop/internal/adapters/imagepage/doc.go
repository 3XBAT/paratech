// Package imagepage makes a photo of a handwritten note safe to keep and show as a page without text (TASK-165,
// ADR-0027 «Handwriting and scans»). Only the standard library is used (image/jpeg, image/png); nothing is sent
// anywhere and no text is recognized.
//
// The bytes are untrusted. The type is told by the signature (material.DetectType), never by a name; HEIC, GIF,
// WebP, BMP, TIFF and SVG are material.ErrUnsupportedImage. Before anything is decoded the size
// (material.MaxImageBytes), the dimensions from the header (material.MaxImagePixels) and the memory the decoder would
// take are checked, and a file too small to hold its pixels (a decompression bomb, or a truncated file) is refused, so
// a header that claims 60000×60000 never allocates. Photos are decoded one at a time.
//
// The photo is then encoded again, JPEG as JPEG and PNG as PNG: EXIF (GPS among it), ICC profiles, comments and text
// chunks are not written, so the clean bytes are what is stored and shown. The EXIF orientation of a JPEG is applied
// to the pixels first, so a phone photo keeps its side up.
package imagepage

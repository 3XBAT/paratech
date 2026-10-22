// Package material is the aggregate of one file of the library (ADR-0024): its sha256, size, detected type, the
// original name (shown only, never sent to a model), where it is filed, the lessons it is linked to, its status and
// the status of its text.
//
// A material without a subject and folder is in «Входящие», where every upload lands. The bytes live once in the
// private artifact store under the sha256 (BlobRef); the type comes from the content, never from the file name.
package material

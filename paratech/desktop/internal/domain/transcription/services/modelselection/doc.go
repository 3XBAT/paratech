// Package modelselection is the domain service that chooses STT models (PRD sections 8 and 8.1, AC-07, AC-08): the
// model an STT job pins, the default that remains after a removal, and the catalog model recommended for a machine.
// It is pure: the caller passes the installed models, the selected default and the machine profile.
//
// Rules that apply: ARCH-10..19.
package modelselection

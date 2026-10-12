// Package senddelivery is the use case of emailing a finished homework artifact after explicit user confirmation:
// creating the delivery, sending it, storing the result and reconciling an unknown outcome instead of retrying
// blindly (PRD sections 10 and 14 case 7, AC-14, AC-15, hard rule 7).
//
// TASK-003 contains only the ports (ports.go). The Service arrives with TASK-044.
//
// Rules that apply: ARCH-20..27. The email adapter is reached only after validation and user confirmation
// (hard rule 3, ARCH-27); the status machine lives in the delivery aggregate.
package senddelivery

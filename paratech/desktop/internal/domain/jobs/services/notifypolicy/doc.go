// Package notifypolicy is the domain service that decides whether a finished job notifies the user (PRD section 15):
// only while the user setting "notify on processing complete" is on, only for a notifiable status (notice.Notifiable)
// and only for the job types the policy was built with, the user-visible processing steps.
//
// It is pure (ARCH-15): the setting comes in as a parameter. The set of types is configured by the caller, because the
// type names belong to other contexts and the domain must not import them (ARCH-11).
package notifypolicy

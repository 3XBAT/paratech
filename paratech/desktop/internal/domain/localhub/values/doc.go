// Package values holds the value objects of the local Hub context (ADR-0036 sections 2, 3, 7): the loopback Target
// parsed from «Адрес сервера», the validated non-secret ServerSettings, the closed list of secret slot names, the probe
// Facts and the Diagnosis (Status, Reason, NextStep) the owner sees. Reasons and next steps are stable codes; the UI
// turns them into Russian text.
//
// Rules that apply: ARCH-10..19.
package values

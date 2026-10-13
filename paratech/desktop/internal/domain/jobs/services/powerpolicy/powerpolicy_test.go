package powerpolicy

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

const (
	typeStt  job.Type = "stt"
	typeMail job.Type = "mail"
	typeLLM  job.Type = "summarize"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

var (
	onAC        = Conditions{OnlyOnACPower: true, Source: SourceAC}
	onBattery   = Conditions{OnlyOnACPower: true, Source: SourceBattery}
	onUnknown   = Conditions{OnlyOnACPower: true, Source: SourceUnknown}
	settingOff  = Conditions{OnlyOnACPower: false, Source: SourceBattery}
	offUnknown  = Conditions{OnlyOnACPower: false, Source: SourceUnknown}
	offOnAC     = Conditions{OnlyOnACPower: false, Source: SourceAC}
	allSituated = []Conditions{onAC, onBattery, onUnknown, settingOff, offUnknown, offOnAC}
)

func mustPolicy(t *testing.T) Policy {
	t.Helper()
	p, err := New([]job.Type{typeLLM, typeMail, typeStt}, []job.Type{typeStt})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSourceOf(t *testing.T) {
	if SourceOf(true) != SourceAC || SourceOf(false) != SourceBattery {
		t.Fatalf("SourceOf(true)=%v SourceOf(false)=%v", SourceOf(true), SourceOf(false))
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name     string
		class    Class
		cond     Conditions
		override bool
		want     Decision
	}{
		{"heavy on AC", ClassHeavy, onAC, false, Allow},
		{"heavy on battery waits (AC-21)", ClassHeavy, onBattery, false, Wait},
		{"heavy on battery with run now (AC-22)", ClassHeavy, onBattery, true, Allow},
		{"heavy on an unknown source counts as AC", ClassHeavy, onUnknown, false, Allow},
		{"heavy on battery with the setting off (AC-22)", ClassHeavy, settingOff, false, Allow},
		{"heavy, setting off, unknown source", ClassHeavy, offUnknown, false, Allow},
		{"heavy, setting off, AC", ClassHeavy, offOnAC, false, Allow},
		{"light on battery is never restricted", ClassLight, onBattery, false, Allow},
		{"light on AC", ClassLight, onAC, false, Allow},
		{"light with run now", ClassLight, onBattery, true, Allow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.class, tc.cond, tc.override); got != tc.want {
				t.Fatalf("Decide = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRestricted(t *testing.T) {
	for _, c := range allSituated {
		want := c == onBattery
		if got := c.Restricted(); got != want {
			t.Errorf("%+v.Restricted() = %v, want %v", c, got, want)
		}
	}
}

func TestNewValidates(t *testing.T) {
	tests := []struct {
		name  string
		types []job.Type
		heavy []job.Type
	}{
		{"no types", nil, nil},
		{"heavy type not claimable", []job.Type{typeMail}, []job.Type{typeStt}},
		{"duplicate type", []job.Type{typeMail, typeMail}, nil},
		{"duplicate heavy", []job.Type{typeStt}, []job.Type{typeStt, typeStt}},
		{"blank type", []job.Type{""}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.types, tc.heavy); !errors.Is(err, job.ErrInvalidClaimFilter) {
				t.Fatalf("New = %v, want ErrInvalidClaimFilter", err)
			}
		})
	}
}

func TestClassOf(t *testing.T) {
	p := mustPolicy(t)
	tests := []struct {
		typ  job.Type
		want Class
	}{
		{typeStt, ClassHeavy},
		{typeMail, ClassLight},
		{typeLLM, ClassLight},
		{"unknown", ClassLight},
	}
	for _, tc := range tests {
		if got := p.ClassOf(tc.typ); got != tc.want {
			t.Errorf("ClassOf(%q) = %v, want %v", tc.typ, got, tc.want)
		}
	}
}

func TestFilterAgreesWithDecide(t *testing.T) {
	p := mustPolicy(t)
	for _, c := range allSituated {
		f := p.Filter(c)
		if f.HeavyAllowed() == c.Restricted() {
			t.Errorf("%+v: HeavyAllowed = %v", c, f.HeavyAllowed())
		}
		for _, typ := range []job.Type{typeStt, typeMail, typeLLM} {
			for _, runNow := range []bool{false, true} {
				want := p.Decide(typ, c, runNow) == Allow
				if got := f.Admits(typ, runNow); got != want {
					t.Errorf("%+v: Admits(%q, runNow=%v) = %v, Decide says %v", c, typ, runNow, got, want)
				}
			}
		}
		if f.Admits("unknown", true) {
			t.Errorf("%+v: an unregistered type is admitted", c)
		}
	}
}

func TestStartedByOverride(t *testing.T) {
	p := mustPolicy(t)
	tests := []struct {
		name string
		typ  job.Type
		cond Conditions
		want bool
	}{
		{"heavy claimed on battery", typeStt, onBattery, true},
		{"heavy claimed on AC", typeStt, onAC, false},
		{"heavy claimed with the setting off", typeStt, settingOff, false},
		{"light claimed on battery", typeMail, onBattery, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.StartedByOverride(tc.typ, tc.cond); got != tc.want {
				t.Fatalf("StartedByOverride = %v, want %v", got, tc.want)
			}
		})
	}
}

func newJob(t *testing.T, typ job.Type, runAt time.Time, runNow bool) *job.Job {
	t.Helper()
	j, err := job.New(job.Spec{ID: shared.JobID("job-1"), Type: typ, Key: "k", MaxAttempts: 3, RunAt: runAt, RunNow: runNow}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func waiting(t *testing.T, typ job.Type) *job.Job {
	t.Helper()
	j := newJob(t, typ, time.Time{}, false)
	if err := j.WaitFor(job.StatusWaitingForPower, t0); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestPark(t *testing.T) {
	p := mustPolicy(t)
	cancelled := newJob(t, typeStt, time.Time{}, false)
	if err := cancelled.Cancel(t0); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		job  *job.Job
		cond Conditions
		want bool
	}{
		{"due heavy on battery", newJob(t, typeStt, time.Time{}, false), onBattery, true},
		{"heavy not due yet keeps its run time", newJob(t, typeStt, t0.Add(time.Hour), false), onBattery, false},
		{"heavy with run now", newJob(t, typeStt, time.Time{}, true), onBattery, false},
		{"heavy on AC", newJob(t, typeStt, time.Time{}, false), onAC, false},
		{"heavy on an unknown source", newJob(t, typeStt, time.Time{}, false), onUnknown, false},
		{"heavy with the setting off", newJob(t, typeStt, time.Time{}, false), settingOff, false},
		{"light on battery", newJob(t, typeMail, time.Time{}, false), onBattery, false},
		{"unknown type on battery", newJob(t, "other", time.Time{}, false), onBattery, false},
		{"not queued", cancelled, onBattery, false},
		{"already waiting", waiting(t, typeStt), onBattery, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Park(tc.job, tc.cond, t0); got != tc.want {
				t.Fatalf("Park = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRelease(t *testing.T) {
	p := mustPolicy(t)
	tests := []struct {
		name string
		job  *job.Job
		cond Conditions
		want bool
	}{
		{"AC is back", waiting(t, typeStt), onAC, true},
		{"unknown source counts as AC", waiting(t, typeStt), onUnknown, true},
		{"setting turned off", waiting(t, typeStt), settingOff, true},
		{"still on battery", waiting(t, typeStt), onBattery, false},
		{"light job waiting for power", waiting(t, typeMail), onBattery, true},
		{"unknown type is left alone", waiting(t, "other"), onAC, false},
		{"queued job", newJob(t, typeStt, time.Time{}, false), onAC, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Release(tc.job, tc.cond); got != tc.want {
				t.Fatalf("Release = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestZeroPolicyAdmitsNothing(t *testing.T) {
	var p Policy
	if !p.Filter(onAC).IsZero() || !p.Filter(onBattery).IsZero() {
		t.Fatal("zero policy filter is not the zero filter")
	}
	if p.Release(waiting(t, typeStt), onAC) {
		t.Fatal("zero policy releases a job")
	}
}

package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// The compensations of set_setting and recording{start} round-trip on real SQLite, the setting pinned to its intent;
// recording{stop} completes DONE without one (TASK-322).
func TestAgentActionsWave10ControlKinds(t *testing.T) {
	db, ctx := openTest(t, nil), context.Background()
	if err := NewAgentThreadRepository(db).Save(ctx, subjectThread(t)); err != nil {
		t.Fatal(err)
	}
	repo := NewAgentActionRepository(db)

	cases := []struct {
		name    string
		tool    values.Tool
		comp    action.Compensation // nil: none
		pinned  bool
		version int64
	}{
		{"toggle back on", values.ToolSetSetting, action.RestoreSetting{Setting: values.SettingACPowerOnly, OldValue: values.SettingOn}, true, 2},
		{"prompts back off", values.ToolSetSetting, action.RestoreSetting{Setting: values.SettingRecordingPrompts, OldValue: values.SettingOff}, true, 1},
		{"load back to gentle", values.ToolSetSetting, action.RestoreSetting{Setting: values.SettingRecognitionLoad, OldValue: "gentle"}, true, 3},
		{"stop the started recording", values.ToolRecording, action.StopRecording{Recording: agentRef(t, values.KindRecording, "rec-1")}, false, 1},
		{"stop: no undo", values.ToolRecording, nil, false, 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := shared.ActionID(fmt.Sprintf("ctl%d", i))
			spec := action.Spec{ID: id, Thread: "th1", Turn: 1, CallID: fmt.Sprintf("k%d", i), Tool: tc.tool}
			if tc.pinned {
				spec.Intent = action.Intent{Prior: tc.comp, Next: tc.comp}
			}
			a, err := action.New(spec, agentAt(i))
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, a); err != nil {
				t.Fatalf("insert: %v", err)
			}
			stored, err := repo.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			comp := tc.comp
			if tc.pinned {
				comp = stored.Intent().Prior
			}
			if err := stored.Complete(comp, tc.version, agentAt(i).Add(time.Second)); err != nil {
				t.Fatalf("Complete after a read: %v", err)
			}
			if err := repo.Save(ctx, stored); err != nil {
				t.Fatalf("update: %v", err)
			}
			got, err := repo.Get(ctx, id)
			if err != nil || !reflect.DeepEqual(got.State(), stored.State()) {
				t.Fatalf("got %+v, %v\nwant %+v", got.State(), err, stored.State())
			}
			if got.Compensation() != tc.comp {
				t.Fatalf("compensation = %#v, want %#v", got.Compensation(), tc.comp)
			}
		})
	}
}

func TestAgentActionsWave10ControlRefusesBadRows(t *testing.T) {
	bad := []string{
		`{"kind":"restore_setting"}`,
		`{"kind":"restore_setting","setting":{"name":"free_work_notice","old_value":"on"}}`,
		`{"kind":"restore_setting","setting":{"name":"recognition_load","old_value":"on"}}`,
		`{"kind":"stop_recording"}`,
	}
	for _, s := range bad {
		var w compensationWire
		if err := strictJSON(s, &w); err != nil {
			t.Fatal(err)
		}
		c, err := compensationOfWire(w)
		if err != nil {
			continue
		}
		// Decoded: the domain refuses it, also as the entry's own journaled prior.
		tool := values.ToolRecording
		if c.Kind() == action.CompRestoreSetting {
			tool = values.ToolSetSetting
		}
		_, err = action.New(action.Spec{ID: "bad", Thread: "th1", Turn: 1, CallID: "b", Tool: tool,
			Intent: action.Intent{Prior: c}}, agentAt(1))
		if err == nil {
			t.Errorf("%s: accepted as the prior of %s", s, tool)
		}
	}
}

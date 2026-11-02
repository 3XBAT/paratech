package wails

import (
	"reflect"
	"testing"
)

// TASK-342: the free-work notice is gone, so the page has no card call to make: a work on a topic runs at once.
func TestAgentServiceHasNoFreeWorkCalls(t *testing.T) {
	for _, m := range []string{"FreeWork", "FreeWorkCard"} {
		if _, ok := reflect.TypeFor[*AgentService]().MethodByName(m); ok {
			t.Errorf("AgentService.%s is bound again", m)
		}
	}
	if _, ok := reflect.TypeFor[*SettingsService]().MethodByName("SetFreeWorkNotice"); ok {
		t.Error("SettingsService.SetFreeWorkNotice is bound again")
	}
}

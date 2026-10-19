package wails

import (
	"context"
	"time"

	"paratech/desktop/internal/cases/sttspeed"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
)

// SpeedEstimates is the recognition time estimate the models and settings pages show (ARCH-42; TASK-084).
type SpeedEstimates interface {
	Overview(ctx context.Context) (sttspeed.Overview, error)
}

// LoadEstimate is the JSON form of the approximate processing time of a 1 h recording in one load mode on this
// computer.
type LoadEstimate struct {
	// Mode: gentle («Бережная»), normal («Обычная») or max («Максимальная»).
	Mode string `json:"mode"`
	// SecondsPerHour is the processing time of 1 h of audio in seconds; 0 when unknown.
	SecondsPerHour int64 `json:"secondsPerHour"`
	// Source: table (rough table, show «примерно»), measured (runs on this computer) or unknown (no number).
	Source string `json:"source"`
	// Heavy: 1 h of audio takes longer than 1 h; the page warns with the number.
	Heavy bool `json:"heavy"`
}

// ModelLoadEstimates are the estimates of one model: in the current mode and in every mode (gentle, normal, max).
type ModelLoadEstimates struct {
	Current LoadEstimate   `json:"current"`
	Modes   []LoadEstimate `json:"modes"`
}

func loadEstimate(e speedestimate.Estimate) LoadEstimate {
	return LoadEstimate{
		Mode: e.Load().String(), SecondsPerHour: int64(e.PerHour().Round(time.Second) / time.Second),
		Source: string(e.Source()), Heavy: e.Heavy(),
	}
}

func modelLoadEstimates(m sttspeed.ModelEstimates) *ModelLoadEstimates {
	out := &ModelLoadEstimates{Current: loadEstimate(m.Current), Modes: make([]LoadEstimate, 0, len(m.Modes))}
	for _, e := range m.Modes {
		out.Modes = append(out.Modes, loadEstimate(e))
	}
	return out
}

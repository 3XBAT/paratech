package testenv

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Scenarios of the fake whisper-cli, chosen by the content of the model file.
const (
	// WhisperOK prints progress, checks the audio file and writes a result in whisper.cpp's JSON format with the
	// language "ru" and WhisperOKSegments.
	WhisperOK = "ok"

	// WhisperHang prints one progress line and then runs until it is killed.
	WhisperHang = "hang"

	// WhisperFail exits with code 3 ("model could not be loaded").
	WhisperFail = "fail"
)

// WhisperOKSegments are the texts of the WhisperOK result, in order.
var WhisperOKSegments = []string{"Добрый день.", "Homework: exercise 5."}

// RunFakeWhisper lets a test binary double as whisper-cli: call it first in TestMain and exit with its code when ok
// is true. The whispercli adapter runs the binary with an empty environment, so the call is recognised by its
// arguments (-m, -f, -of and -oj, never present in a go test command line); ok is false for anything else.
func RunFakeWhisper(args []string) (code int, ok bool) {
	opts := map[string]string{}
	flags := map[string]bool{}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-m", "-f", "-of", "-t", "-l":
			if i+1 >= len(args) {
				return 0, false
			}
			opts[a] = args[i+1]
			i++
		case "-oj", "-pp", "-ng":
			flags[a] = true
		default:
			return 0, false
		}
	}
	if opts["-m"] == "" || opts["-f"] == "" || opts["-of"] == "" || !flags["-oj"] {
		return 0, false
	}

	raw, err := os.ReadFile(opts["-m"])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: failed to open model")
		return 3, true
	}

	switch strings.TrimSpace(string(raw)) {
	case WhisperOK:
		if _, err := os.Stat(opts["-f"]); err != nil {
			fmt.Fprintln(os.Stderr, "error: input file not found")
			return 2, true
		}
		for p := 0; p <= 100; p += 25 {
			fmt.Fprintf(os.Stderr, "whisper_print_progress_callback: progress = %3d%%\n", p)
		}
		if err := os.WriteFile(opts["-of"]+".json", whisperResult(WhisperOKSegments), 0o600); err != nil {
			return 11, true
		}
		return 0, true

	case WhisperHang:
		fmt.Fprintln(os.Stderr, "whisper_print_progress_callback: progress =  10%")
		// A sleeping loop, not select {}: with every goroutine blocked the runtime would exit with a deadlock.
		for {
			time.Sleep(time.Hour)
		}

	case WhisperFail:
		fmt.Fprintln(os.Stderr, "whisper_model_load: invalid model data (bad magic)")
		return 3, true
	}

	fmt.Fprintln(os.Stderr, "fake: unknown scenario")
	return 99, true
}

// whisperResult renders the parts of the -oj output of whisper.cpp v1.9.5 that the adapter reads.
func whisperResult(texts []string) []byte {
	type offsets struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	}
	type segment struct {
		Offsets offsets `json:"offsets"`
		Text    string  `json:"text"`
	}
	segs := make([]segment, len(texts))
	for i, t := range texts {
		segs[i] = segment{Offsets: offsets{From: int64(i) * 2000, To: int64(i+1) * 2000}, Text: " " + t}
	}
	data, _ := json.Marshal(map[string]any{
		"result":        map[string]string{"language": "ru"},
		"transcription": segs,
	})
	return data
}

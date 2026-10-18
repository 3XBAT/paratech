package whispercli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake whisper-cli: the engine runs it with fakeEnvKey set (the only variable the
// test engine passes) and the scenario is the content of the model file.
const (
	fakeEnvKey   = "WHISPERCLI_TEST_FAKE"
	fakeChildKey = "WHISPERCLI_TEST_FAKE_CHILD"
	fakeOn       = "1"
)

// Fake scenarios.
const (
	modeOK       = "ok"
	modeEcho     = "echo"
	modeEnv      = "env"
	modeExit3    = "exit3"
	modeNoOut    = "noout"
	modeHang     = "hang"
	modeTree     = "tree"
	modeGarbage  = "garbage"
	modeHuge     = "huge"
	modeBadTS    = "badts"
	modeTrailing = "trailing"
	modeMissing  = "missing"
	modeSpam     = "spam"
	modeEmpty    = "empty"
	modeFlood    = "flood"
	modeLive     = "live"
)

// Exit codes of the fake that point at a broken argument vector.
const (
	fakeExitPositional = 41
	fakeExitNoModel    = 42
)

const (
	beatSuffix      = ".beat"
	childBeatSuffix = ".child.beat"
	beatEvery       = 10 * time.Millisecond
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnvKey) == fakeOn {
		os.Exit(fakeWhisper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type fakeArgs struct {
	model, file, out, threads, lang string
	flags                           []string
	positional                      []string
}

// parseFakeArgs follows examples/cli/cli.cpp: a value flag consumes the next argument verbatim, anything not
// starting with '-' (or "-" itself) is an input file, an unknown flag ends the program with code 0.
func parseFakeArgs(args []string) (fakeArgs, bool) {
	var a fakeArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			a.positional = append(a.positional, arg)
			continue
		}

		value := func(dst *string) bool {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: argument %s requires value\n", arg)
				return false
			}
			i++
			*dst = args[i]
			return true
		}

		ok := true
		switch arg {
		case flagModel:
			ok = value(&a.model)
		case flagFile:
			ok = value(&a.file)
		case flagOutputFile:
			ok = value(&a.out)
		case flagThreads:
			ok = value(&a.threads)
		case flagLanguage:
			ok = value(&a.lang)
		case flagOutputJSON, flagPrintProgress, flagNoGPU:
			a.flags = append(a.flags, arg)
		default:
			fmt.Fprintf(os.Stderr, "error: unknown argument: %s\n", arg)
			ok = false
		}
		if !ok {
			return a, false
		}
	}
	return a, true
}

func fakeWhisper(args []string) int {
	a, ok := parseFakeArgs(args)
	if !ok {
		return 0
	}
	if len(a.positional) > 0 {
		return fakeExitPositional
	}

	raw, err := os.ReadFile(a.model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: model file not found '%s'\n", a.model)
		return fakeExitNoModel
	}
	mode := strings.TrimSpace(string(raw))
	result := a.out + jsonExt

	// The grandchild of the tree scenario only runs until it is killed.
	if os.Getenv(fakeChildKey) == fakeOn {
		heartbeat(a.model + childBeatSuffix)
		select {}
	}

	switch mode {
	case modeOK:
		fmt.Fprintf(os.Stderr, "whisper_init_from_file_with_params_no_state: loading model from '%s'\n", native(a.model))
		fmt.Fprintf(os.Stderr, "main: processing '%s' (160000 samples, 10.0 sec), %s threads, lang = %s\n", native(a.file), a.threads, a.lang)
		for p := 0; p <= 100; p += 5 {
			fmt.Fprintf(os.Stderr, "whisper_print_progress_callback: progress = %3d%%\n", p)
		}
		writeFile(result, whisperJSON(a.model, []fakeSeg{
			{0, 2500, " Hello, \"world\" \\ ok."},
			{2500, 5000, " second\tline\nwith a raw newline"},
		}))
		fmt.Fprintf(os.Stderr, "output_json: saving output to '%s'\n", native(result))
		return 0

	case modeEcho:
		cwd, _ := os.Getwd()
		texts := []string{a.model, a.file, a.out, a.threads, a.lang, cwd, strings.Join(a.flags, " ")}
		writeFile(result, jsonOf(texts))
		return 0

	case modeEnv:
		env := os.Environ()
		sort.Strings(env)
		writeFile(result, jsonOf(env))
		return 0

	case modeExit3:
		fmt.Fprintf(os.Stderr, "whisper_init_from_file_with_params_no_state: loading model from '%s'\n", native(a.model))
		fmt.Fprintf(os.Stderr, "whisper_model_load: invalid model data (bad magic) in %s\n", native(strings.ToUpper(a.model)))
		fmt.Fprintf(os.Stderr, "error: failed to initialize whisper context\n")
		return 3

	case modeNoOut:
		fmt.Fprintf(os.Stderr, "error: unknown language 'xx'\n")
		return 0

	case modeHang:
		writeFile(result, `{"transcription": [`)
		heartbeat(a.model + beatSuffix)
		fmt.Fprintf(os.Stderr, "whisper_print_progress_callback: progress =  10%%\n")
		select {}

	case modeTree:
		child := exec.Command(os.Args[0], os.Args[1:]...) //nolint:gosec // test helper
		child.Env = append(os.Environ(), fakeChildKey+"="+fakeOn)
		if err := child.Start(); err != nil {
			return 50
		}
		waitFile(a.model+childBeatSuffix, 10*time.Second)
		heartbeat(a.model + beatSuffix)
		fmt.Fprintf(os.Stderr, "whisper_print_progress_callback: progress =  10%%\n")
		select {}

	case modeFlood:
		// Far more than a pipe buffer after the first progress line: a blocked reader would stall the process.
		noise := strings.Repeat("n", 200) + "\n"
		for p := 0; p <= 100; p += 5 {
			fmt.Fprintf(os.Stderr, "whisper_print_progress_callback: progress = %3d%%\n", p)
			for range (64 << 10) / len(noise) {
				fmt.Fprint(os.Stderr, noise)
			}
		}
		writeFile(result, whisperJSON(a.model, []fakeSeg{{0, 1000, "flood"}}))
		return 0

	case modeLive:
		// The segment lines of whisper_print_segment_callback, written piecemeal like a flushed printf, between
		// lines the adapter must drop.
		writeOut("\n")
		for _, chunk := range []string{
			"[00:00:00.000 --> 00:00:02.500]  Привет",
			", мир.\r\n",
			"not a segment line\n",
			"[00:00:02.500 --> 00:00:04.000]  ",
			"\xd0", "\x9f", "ока\n",
			"[00:00:01.000 --> 00:00:01.500]  earlier start\n",
			"[00:00:05.000 --> 00:00:06.000]  " + strings.Repeat("x", 20<<10) + "\n",
			"[00:00:06.000 --> 00:00:07.000]  bad \xff byte\n",
			"[00:00:07.000 --> 00:00:08.000]   \n",
			"[01:02:03.004 --> 01:02:05.000]  last",
		} {
			writeOut(chunk)
			time.Sleep(time.Millisecond)
		}
		writeOut("\n")
		writeFile(result, whisperJSON(a.model, []fakeSeg{{0, 2500, " Привет, мир."}, {2500, 4000, " Пока"}}))
		return 0

	case modeGarbage:
		writeFile(result, "not json {")
		return 0

	case modeHuge:
		writeFile(result, whisperJSON(a.model, []fakeSeg{{0, 1000, strings.Repeat("x", 1<<20)}}))
		return 0

	case modeBadTS:
		writeFile(result, whisperJSON(a.model, []fakeSeg{{-5, 1000, "negative"}}))
		return 0

	case modeTrailing:
		writeFile(result, whisperJSON(a.model, nil)+" {}")
		return 0

	case modeMissing:
		writeFile(result, `{"transcription": [{"text": "no offsets"}]}`)
		return 0

	case modeSpam:
		line := fmt.Sprintf("log line with the model path %s\n", native(a.model))
		for range (1 << 20) / len(line) {
			fmt.Fprint(os.Stderr, line)
		}
		fmt.Fprint(os.Stderr, strings.Repeat("y", 200<<10))
		fmt.Fprintf(os.Stderr, "\nerror: input file(s) failed near %s\n", native(a.file))
		return 1

	case modeEmpty:
		writeFile(result, whisperJSON(a.model, nil))
		return 0
	}

	fmt.Fprintf(os.Stderr, "fake: unknown mode %q\n", mode)
	return 99
}

type fakeSeg struct {
	from, to int64
	text     string
}

// whisperJSON imitates output_json of cli.cpp: tabs, only quotes and backslashes escaped in strings.
func whisperJSON(model string, segs []fakeSeg) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`)

	var b strings.Builder
	b.WriteString("{\n\t\"systeminfo\": \"AVX = 1 | FMA = 1\",\n")
	b.WriteString("\t\"model\": {\n\t\t\"type\": \"base\",\n\t\t\"multilingual\": true\n\t},\n")
	fmt.Fprintf(&b, "\t\"params\": {\n\t\t\"model\": \"%s\",\n\t\t\"language\": \"auto\",\n\t\t\"translate\": false\n\t},\n", esc.Replace(model))
	b.WriteString("\t\"result\": {\n\t\t\"language\": \"ru\"\n\t},\n")
	b.WriteString("\t\"transcription\": [\n")
	for i, s := range segs {
		b.WriteString("\t\t{\n")
		b.WriteString("\t\t\t\"timestamps\": {\n\t\t\t\t\"from\": \"00:00:00,000\",\n\t\t\t\t\"to\": \"00:00:00,000\"\n\t\t\t},\n")
		fmt.Fprintf(&b, "\t\t\t\"offsets\": {\n\t\t\t\t\"from\": %d,\n\t\t\t\t\"to\": %d\n\t\t\t},\n", s.from, s.to)
		fmt.Fprintf(&b, "\t\t\t\"text\": \"%s\"\n", esc.Replace(s.text))
		if i == len(segs)-1 {
			b.WriteString("\t\t}\n")
		} else {
			b.WriteString("\t\t},\n")
		}
	}
	b.WriteString("\t]\n}\n")
	return b.String()
}

// jsonOf returns a result whose segment texts are the given strings, in order.
func jsonOf(texts []string) string {
	type off struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	}
	type seg struct {
		Offsets off    `json:"offsets"`
		Text    string `json:"text"`
	}
	segs := make([]seg, len(texts))
	for i, t := range texts {
		segs[i] = seg{Offsets: off{int64(i), int64(i)}, Text: t}
	}
	data, _ := json.Marshal(map[string]any{"transcription": segs})
	return string(data)
}

// native returns s as whisper-cli prints it (ANSI code page bytes on Windows).
func native(s string) string {
	if n, ok := nativeForm(s); ok {
		return n
	}
	return s
}

// writeOut writes s to stdout as one write, like a flushed printf.
func writeOut(s string) {
	_, _ = os.Stdout.WriteString(s)
}

func writeFile(path, content string) {
	_ = os.WriteFile(path, []byte(content), 0o600)
}

// heartbeat appends to path forever, so a test can see whether the process still runs. The first beat is
// written before it returns.
func heartbeat(path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test helper
	if err != nil {
		return
	}
	_, _ = f.WriteString(".")

	go func() {
		for {
			_, _ = f.WriteString(".")
			time.Sleep(beatEvery)
		}
	}()
}

func waitFile(path string, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
			return
		}
		time.Sleep(beatEvery)
	}
}

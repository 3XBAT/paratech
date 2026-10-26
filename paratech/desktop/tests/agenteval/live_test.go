//go:build agentlive

package agenteval

// Live run of the case set against a real Hub (level 2). Never in CI: it needs the build tag agentlive, the flag
// PARATECH_AGENT_LIVE=1, a Hub URL and a tester key from the environment (README.md). It spends the key's quota.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/testenv"
)

// Environment of the live run. There are no defaults: nothing points at a Hub unless the caller says so.
const (
	envFlag   = "PARATECH_AGENT_LIVE"         // must be 1
	envURL    = "PARATECH_AGENT_LIVE_HUB_URL" // https, or http to loopback
	envKey    = "PARATECH_AGENT_LIVE_KEY"     // a tester key of that Hub
	envReport = "PARATECH_AGENT_LIVE_REPORT"  // optional: write the markdown report to this file
	envOnly   = "PARATECH_AGENT_LIVE_ONLY"    // optional: comma-separated case IDs
)

func TestAgentLive(t *testing.T) {
	if os.Getenv(envFlag) != "1" {
		t.Skipf("set %s=1, %s and %s to run the live agent check", envFlag, envURL, envKey)
	}
	url, key := os.Getenv(envURL), os.Getenv(envKey)
	if url == "" || key == "" {
		t.Fatalf("%s and %s are required", envURL, envKey)
	}

	rawCatalog, err := os.ReadFile(filepath.Join("..", "..", "..", "shared", "testdata", CatalogFile))
	if err != nil {
		t.Fatal(err)
	}
	classes, err := CatalogClasses(rawCatalog)
	if err != nil {
		t.Fatal(err)
	}
	cs := loadCases(t)

	// The key lives only in this in-memory store, never in the Windows credential store.
	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), hubclient.DefaultCredentialName, key); err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(hubclient.Config{BaseURL: url, RequestTimeout: time.Minute}, creds)
	if err != nil {
		t.Fatal(err)
	}
	r := &liveRun{client: client, classes: classes, id: strconv.FormatInt(time.Now().Unix(), 36)}
	if r.base, err = liveBase(cs.Subjects); err != nil {
		t.Fatal(err)
	}

	only := map[string]bool{}
	for _, id := range strings.Split(os.Getenv(envOnly), ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}
	pick := func(id string) bool { return len(only) == 0 || only[id] }

	report := Report{Classes: classes}
	for _, req := range slices.Concat(cs.Requests, cs.CannotDo) {
		if pick(req.ID) {
			report.Requests = append(report.Requests, r.request(req))
		}
	}
	for _, q := range cs.Questions {
		if !pick(q.ID) {
			continue
		}
		o := r.request(Request{ID: q.ID, Text: q.Text, Subject: q.Subject, Class: ClassRead, Tools: []string{"answer_question"}})
		// Citations are measured once the Hub has the ANSWER prompt and the desktop its answering case (TASK-118).
		report.Questions = append(report.Questions, QuestionOutcome{Question: q, Tools: o.Tools, Err: o.Err})
	}

	md := report.Markdown()
	if path := os.Getenv(envReport); path != "" {
		if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println(md)
}

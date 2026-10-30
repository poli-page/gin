package main

// Conformance with the Poli Page API contract (https://github.com/poli-page/api-contract).
//
// CI copies the pinned contract.json with the poli-page/api-contract action and sets
// POLI_CONTRACT to its path. Every `kind: "impl"` case runs against this example app: a local
// HTTP server answers the case's responses in order and records each request, the app's SDK
// client points at it through polipagegin.Config (BaseURL), the case's request goes through the
// app's Gin router, and the test compares the app's answer (status, Location, Content-Type,
// JSON body) and the recorded SDK requests with the case's `expect`. `kind: "sdk"` cases belong
// to the SDK and are ignored here.
//
// The SDK keeps its default retry settings: the cases expect the default number of attempts.
// No policy.normalizations entry applies: the app's JSON body is compared as is (its unknown
// status and requestId are already null), and no case compares bytes.
//
// testdata/contract-skip.json maps the case ids allowed to fail to the reason. A listed case
// that passes, or a listed id the contract has no impl case for, fails the run.
//
// Locally: `POLI_CONTRACT=/path/to/contract.json go test -run TestContract .` from
// example-app/. Without it the test is skipped, except when CI is set, where a missing contract
// is an error.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	polipagegin "github.com/poli-page/gin"
)

// contractFormatVersion is the contract.json layout this file reads (README of
// poli-page/api-contract).
const contractFormatVersion = 1

// contractTestAPIKey is the API key the app's SDK client sends to the local server.
const contractTestAPIKey = "pp_sandbox_0123456789abcdef0123456789abcdef"

// contractSkipFile lists the case ids allowed to fail, with the reason.
const contractSkipFile = "testdata/contract-skip.json"

// contractCaseTimeout bounds one case; the slowest (three attempts with the default backoff)
// takes about 2 s.
const contractCaseTimeout = 30 * time.Second

var (
	placeholderPattern = regexp.MustCompile(`\{\{([^}]*)\}\}`)
	httpDatePattern    = regexp.MustCompile(`^httpDate\+(\d+)$`)
	uuidV4Pattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// ---------------------------------------------------------------------------
// Contract format 1
// ---------------------------------------------------------------------------

type contractFile struct {
	Cases []json.RawMessage `json:"cases"`
}

type caseHeader struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type implCase struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Request struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"request"`
	Responses []mockStep `json:"responses"`
	Expect    struct {
		Status      int                `json:"status"`
		Location    *string            `json:"location"`
		ContentType *string            `json:"contentType"`
		JSON        json.RawMessage    `json:"json"`
		Requests    *[]expectedRequest `json:"requests"`
	} `json:"expect"`
}

type expectedRequest struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Auth           string `json:"auth"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type mockStep struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	JSON    json.RawMessage   `json:"json"`
	Text    *string           `json:"text"`
	Base64  *string           `json:"base64"`
	Fault   string            `json:"fault"`
}

func loadContract(path string) (*contractFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		FormatVersion *int `json:"formatVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("%s is not JSON: %w", path, err)
	}
	if probe.FormatVersion == nil {
		return nil, fmt.Errorf("%s is not a contract.json: it has no formatVersion", path)
	}
	if *probe.FormatVersion != contractFormatVersion {
		return nil, fmt.Errorf("contract.json formatVersion %d is not supported (this test reads format %d): update contract_test.go",
			*probe.FormatVersion, contractFormatVersion)
	}
	var c contractFile
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// prepareCase replaces the placeholders of every string in the case, then decodes it strictly:
// a field this file does not know fails the case instead of being ignored.
func prepareCase(raw json.RawMessage, origin string) (*implCase, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	substituted, err := substitute(decoded, origin)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(substituted)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.DisallowUnknownFields()
	var c implCase
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if c.Request.Method == "" || !strings.HasPrefix(c.Request.Path, "/") {
		return nil, fmt.Errorf("request needs a method and an absolute path, got %q %q", c.Request.Method, c.Request.Path)
	}
	if c.Expect.Status == 0 {
		return nil, fmt.Errorf("expect has no status")
	}
	if c.Expect.Requests != nil {
		for i, r := range *c.Expect.Requests {
			if r.Auth != "bearer" && r.Auth != "none" {
				return nil, fmt.Errorf("expect.requests[%d]: unknown auth %q", i, r.Auth)
			}
			if r.IdempotencyKey != "stable" && r.IdempotencyKey != "absent" {
				return nil, fmt.Errorf("expect.requests[%d]: unknown idempotencyKey %q", i, r.IdempotencyKey)
			}
		}
	}
	for i, step := range c.Responses {
		bodies := 0
		if step.JSON != nil {
			bodies++
		}
		if step.Text != nil {
			bodies++
		}
		if step.Base64 != nil {
			bodies++
		}
		switch {
		case step.Fault != "" && step.Fault != "drop" && step.Fault != "hang":
			return nil, fmt.Errorf("responses[%d]: unknown fault %q", i, step.Fault)
		case step.Fault != "" && (bodies > 0 || step.Status != 0 || step.Headers != nil):
			return nil, fmt.Errorf("responses[%d]: a fault has no status, headers or body", i)
		case step.Fault == "" && step.Status == 0:
			return nil, fmt.Errorf("responses[%d]: no status", i)
		case bodies > 1:
			return nil, fmt.Errorf("responses[%d]: more than one of json, text, base64", i)
		}
		if step.Base64 != nil {
			if _, err := base64.StdEncoding.DecodeString(*step.Base64); err != nil {
				return nil, fmt.Errorf("responses[%d].base64: %w", i, err)
			}
		}
	}
	return &c, nil
}

func loadSkipList() (map[string]string, error) {
	raw, err := os.ReadFile(contractSkipFile)
	if err != nil {
		return nil, err
	}
	var entries map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, fmt.Errorf("%s must be an object of case id to reason", contractSkipFile)
	}
	skip := make(map[string]string, len(entries))
	for id, reason := range entries {
		text, ok := reason.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("%s: %q needs a reason", contractSkipFile, id)
		}
		skip[id] = text
	}
	return skip, nil
}

// ---------------------------------------------------------------------------
// Placeholders: {{mock}}, {{httpDate+N}}
// ---------------------------------------------------------------------------

func substituteString(s, mock string) (string, error) {
	var bad error
	out := placeholderPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2]
		if name == "mock" {
			return mock
		}
		if m := httpDatePattern.FindStringSubmatch(name); m != nil {
			secs, err := strconv.Atoi(m[1])
			if err != nil {
				bad = fmt.Errorf("placeholder %s: %w", match, err)
				return match
			}
			// An HTTP-date has whole seconds: round to the nearest one.
			at := time.Now().Add(time.Duration(secs) * time.Second).Round(time.Second)
			return at.UTC().Format(http.TimeFormat)
		}
		bad = fmt.Errorf("unknown contract placeholder %s", match)
		return match
	})
	return out, bad
}

// substitute replaces the placeholders of every string in a decoded JSON value.
func substitute(v any, mock string) (any, error) {
	switch x := v.(type) {
	case string:
		return substituteString(x, mock)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			s, err := substitute(item, mock)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			s, err := substitute(item, mock)
			if err != nil {
				return nil, err
			}
			out[k] = s
		}
		return out, nil
	default:
		return v, nil
	}
}

// ---------------------------------------------------------------------------
// Mock server
// ---------------------------------------------------------------------------

type recordedRequest struct {
	method string
	path   string
	header http.Header
}

type mockServer struct {
	srv   *httptest.Server
	steps []mockStep
	done  chan struct{}

	mu       sync.Mutex
	requests []recordedRequest
	problems []string
}

// newMockServer listens on 127.0.0.1:0 without serving yet, so the case's placeholders can be
// replaced with its origin before its responses are set.
func newMockServer() *mockServer {
	m := &mockServer{done: make(chan struct{})}
	m.srv = httptest.NewUnstartedServer(http.HandlerFunc(m.serve))
	// One connection per request, because this mock serves the API and the presigned download
	// on one origin, where production storage is another host. With keep-alive the download
	// would reuse the render's connection, and net/http's Transport resends a bodyless GET once
	// when a reused connection dies before any answer, which would record a request the SDK
	// never made.
	m.srv.Config.SetKeepAlivesEnabled(false)
	return m
}

func (m *mockServer) origin() string { return "http://" + m.srv.Listener.Addr().String() }

func (m *mockServer) start(steps []mockStep) {
	m.steps = steps
	m.srv.Start()
}

func (m *mockServer) recorded() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

func (m *mockServer) serverProblems() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.problems)
}

func (m *mockServer) close() {
	close(m.done)
	m.srv.CloseClientConnections()
	m.srv.Close()
}

func (m *mockServer) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{
		method: r.Method,
		path:   r.RequestURI,
		header: r.Header.Clone(),
	})
	index := len(m.requests) - 1
	m.mu.Unlock()

	if index >= len(m.steps) {
		// More requests than the case has answers: the request list shows it.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(599)
		_, _ = w.Write([]byte(`{"error":"MOCK_EXHAUSTED"}`))
		return
	}
	step := m.steps[index]
	switch step.Fault {
	case "drop":
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		m.problem("cannot drop the connection of request %d", index+1)
	case "hang":
		select {
		case <-r.Context().Done():
		case <-m.done:
		}
	default:
		if err := m.answer(w, step); err != nil {
			m.problem("answer %d: %v", index+1, err)
		}
	}
}

func (m *mockServer) problem(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.problems = append(m.problems, fmt.Sprintf(format, args...))
}

// answer writes one step; its placeholders were replaced when the case was prepared.
func (m *mockServer) answer(w http.ResponseWriter, step mockStep) error {
	for name, value := range step.Headers {
		w.Header().Set(name, value)
	}
	var body []byte
	switch {
	case step.JSON != nil:
		body = step.JSON
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
	case step.Text != nil:
		body = []byte(*step.Text)
	case step.Base64 != nil:
		body, _ = base64.StdEncoding.DecodeString(*step.Base64)
	}
	w.WriteHeader(step.Status)
	_, err := w.Write(body)
	return err
}

// ---------------------------------------------------------------------------
// Comparing
// ---------------------------------------------------------------------------

// describeRequests turns the recorded requests into the contract's terms: auth is "bearer"
// for exactly `Authorization: Bearer <key>` and "none" without the header; idempotencyKey is
// "stable" for a lowercase UUID v4 identical across the attempts of the one call that sends
// it, "absent" without the header.
func describeRequests(recorded []recordedRequest, apiKey string) []expectedRequest {
	out := make([]expectedRequest, 0, len(recorded))
	firstKey := ""
	for _, req := range recorded {
		auth := "none"
		if values, ok := req.header["Authorization"]; ok {
			auth = "unexpected Authorization: " + strings.Join(values, ", ")
			if len(values) == 1 && values[0] == "Bearer "+apiKey {
				auth = "bearer"
			}
		}
		idempotencyKey := "absent"
		if values, ok := req.header["Idempotency-Key"]; ok {
			key := strings.Join(values, ", ")
			switch {
			case !uuidV4Pattern.MatchString(key):
				idempotencyKey = "not a lowercase UUID v4: " + key
			case firstKey == "" || key == firstKey:
				firstKey = key
				idempotencyKey = "stable"
			default:
				idempotencyKey = "changed: " + firstKey + " then " + key
			}
		}
		out = append(out, expectedRequest{
			Method:         req.method,
			Path:           req.path,
			Auth:           auth,
			IdempotencyKey: idempotencyKey,
		})
	}
	return out
}

func compare(label string, got, want any) string {
	if reflect.DeepEqual(got, want) {
		return ""
	}
	gotJSON, _ := json.MarshalIndent(got, "  ", "  ")
	wantJSON, _ := json.MarshalIndent(want, "  ", "  ")
	return fmt.Sprintf("%s differ\n  want: %s\n  got:  %s", label, wantJSON, gotJSON)
}

// runCase runs one impl case against the example app and returns what differs from the case.
func runCase(raw json.RawMessage) []string {
	server := newMockServer()
	origin := server.origin()
	c, err := prepareCase(raw, origin)
	if err != nil {
		server.srv.Close()
		return []string{"cannot read the case: " + err.Error()}
	}
	server.start(c.Responses)

	router, err := newRouter(polipagegin.Config{APIKey: contractTestAPIKey, BaseURL: origin})
	if err != nil {
		server.close()
		return []string{"cannot build the example app: " + err.Error()}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(c.Request.Method, c.Request.Path, http.NoBody)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		router.ServeHTTP(w, req)
	}()
	select {
	case <-finished:
	case <-time.After(contractCaseTimeout):
		server.close()
		return []string{fmt.Sprintf("the app did not answer within %v", contractCaseTimeout)}
	}
	server.close()

	problems := server.serverProblems()
	if w.Code != c.Expect.Status {
		problems = append(problems, fmt.Sprintf("status: want %d, got %d (body %s)", c.Expect.Status, w.Code, w.Body.String()))
	}
	if c.Expect.Location != nil {
		if got := w.Header().Get("Location"); got != *c.Expect.Location {
			problems = append(problems, fmt.Sprintf("location: want %q, got %q", *c.Expect.Location, got))
		}
	}
	if c.Expect.ContentType != nil {
		got, _, err := mime.ParseMediaType(w.Header().Get("Content-Type"))
		if err != nil {
			got = "unparsable Content-Type: " + w.Header().Get("Content-Type")
		}
		if got != *c.Expect.ContentType {
			problems = append(problems, fmt.Sprintf("contentType: want %q, got %q", *c.Expect.ContentType, got))
		}
	}
	if c.Expect.JSON != nil {
		var want, got any
		if err := json.Unmarshal(c.Expect.JSON, &want); err != nil {
			problems = append(problems, "expect.json: "+err.Error())
		} else if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			problems = append(problems, "body is not JSON: "+w.Body.String())
		} else if diff := compare("json", got, want); diff != "" {
			problems = append(problems, diff)
		}
	}
	if c.Expect.Requests != nil {
		if diff := compare("requests", describeRequests(server.recorded(), contractTestAPIKey), *c.Expect.Requests); diff != "" {
			problems = append(problems, diff)
		}
	}
	return problems
}

func TestContract(t *testing.T) {
	path := os.Getenv("POLI_CONTRACT")
	if path == "" {
		if os.Getenv("CI") != "" {
			t.Fatal(`POLI_CONTRACT is not set: in CI the "API contract" step must provide contract.json (poli-page/api-contract action, output "path")`)
		}
		t.Skip("API contract tests skipped: set POLI_CONTRACT to a contract.json path")
	}
	contract, err := loadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	skip, err := loadSkipList()
	if err != nil {
		t.Fatal(err)
	}

	type namedCase struct {
		id, title string
		raw       json.RawMessage
	}
	var cases []namedCase
	ids := map[string]bool{}
	for i, raw := range contract.Cases {
		var head struct {
			caseHeader
			Title string `json:"title"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatalf("cases[%d]: %v", i, err)
		}
		switch head.Kind {
		case "impl":
			if ids[head.ID] {
				t.Errorf("cases[%d]: duplicate impl case id %q", i, head.ID)
				continue
			}
			ids[head.ID] = true
			cases = append(cases, namedCase{head.ID, head.Title, raw})
		case "sdk":
			// The SDK's own cases: its repository runs them.
		default:
			t.Errorf("cases[%d] (%s) has kind %q: this test knows only \"sdk\" and \"impl\"", i, head.ID, head.Kind)
		}
	}
	if len(cases) == 0 {
		t.Fatal(`the contract has no "impl" case`)
	}
	for id := range skip {
		if !ids[id] {
			t.Errorf("%s lists %q, which the contract has no impl case for: remove it from %s", contractSkipFile, id, contractSkipFile)
		}
	}

	gin.SetMode(gin.TestMode)
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			problems := runCase(c.raw)
			reason, listed := skip[c.id]
			switch {
			case !listed && len(problems) > 0:
				t.Errorf("%s\n%s", c.title, strings.Join(problems, "\n"))
			case listed && len(problems) == 0:
				t.Errorf("%s passes now: remove it from %s", c.id, contractSkipFile)
			case listed:
				t.Skipf("expected to fail (%s): %s", reason, strings.Join(problems, "\n"))
			}
		})
	}
}

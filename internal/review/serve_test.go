package review

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/cards"
)

const serveGraph = `---
config:
  look: classic
  darkMode: true
  theme: dark
  layout: elk
  elk:
    mergeEdges: true
    nodePlacementStrategy: NETWORK_SIMPLEX
---
flowchart TB
    subgraph passed["Concepts User understands"]
        done["Done concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 3
        todo["Todo concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`

const (
	testToken = "0123456789abcdef0123456789abcdef"
	testHost  = "127.0.0.1:8765"
)

// site is a handler over a fresh graph with three cards: 1 and 2 loose,
// 3 waiting on the untested concept.
type site struct {
	t    *testing.T
	h    *handler
	path string
}

func newSite(t *testing.T) *site {
	t.Helper()
	file := filepath.Join(t.TempDir(), "g.tm.mmd")
	if err := os.WriteFile(file, []byte(serveGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	err := cards.Append(cards.Path(file), now.Add(-time.Hour),
		cards.Row{Ev: cards.EvCard, N: 1, Origin: cards.OriginUser, Front: "<b>front 1</b>", Back: "back 1"},
		cards.Row{Ev: cards.EvCard, N: 2, Concept: "done", Origin: cards.OriginSource, Front: "front 2", Back: "back 2", Src: "abc@src.txt:1-2"},
		cards.Row{Ev: cards.EvCard, N: 3, Concept: "todo", Origin: cards.OriginTeacher, Front: "front 3", Back: "back 3"},
	)
	if err != nil {
		t.Fatal(err)
	}
	st := &Store{
		GraphFile: file,
		Set:       cards.Settings{Retention: 0.9, New: 20, Leech: 4},
		Now:       func() time.Time { return now },
		Rng:       rand.New(rand.NewPCG(1, 2)),
	}
	return &site{t: t, h: newHandler(st, testToken, []string{testHost, "localhost:8765", "cards.example:9000"}), path: cards.Path(file)}
}

// do sends one request. A non-empty body is sent as JSON.
func (s *site) do(method, path, body string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, "http://"+testHost+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

// api sends a request under the token and decodes the JSON answer.
func (s *site) api(method, route, body string, want int) map[string]any {
	s.t.Helper()
	rec := s.do(method, "/"+testToken+"/"+route, body)
	if rec.Code != want {
		s.t.Fatalf("%s %s: status %d, want %d: %s", method, route, rec.Code, want, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		s.t.Fatalf("%s %s: not JSON: %s", method, route, rec.Body)
	}
	return out
}

func (s *site) file() string {
	s.t.Helper()
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

func TestHandler_TokenAndHost(t *testing.T) {
	s := newSite(t)
	for _, path := range []string{"/", "/api/next", "/" + testToken, "/wrong/", "/" + testToken + "x/", "/" + testToken + "/nothing", "/" + testToken + "/api/nothing"} {
		if rec := s.do("GET", path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, rec.Code)
		}
	}
	if rec := s.do("POST", "/"+testToken+"/api/nothing", "{}"); rec.Code != http.StatusNotFound {
		t.Errorf("POST to an unknown route: status %d", rec.Code)
	}

	for host, want := range map[string]int{
		testHost:             200,
		"localhost:8765":     200,
		"LOCALHOST:8765":     200,
		"cards.example:9000": 200,
		"localhost:9999":     403,
		"127.0.0.1":          403,
		"evil.example:8765":  403,
		"cards.example":      403,
		"":                   403,
	} {
		req := httptest.NewRequest("GET", "http://x/"+testToken+"/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: status %d, want %d", host, rec.Code, want)
		}
	}
}

func TestHandler_Page(t *testing.T) {
	s := newSite(t)
	rec := s.do("GET", "/"+testToken+"/", "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("page: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q; the token in the URL would leak", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
	body := rec.Body.String()
	// The page loads nothing from the network and never builds HTML from
	// card text.
	for _, banned := range []string{"http://", "https://", "//cdn", "innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "<link", "src="} {
		if strings.Contains(body, banned) {
			t.Errorf("the page contains %q", banned)
		}
	}
	if strings.Contains(body, "front 1") {
		t.Errorf("card text is in the page itself; it should come from the API")
	}
}

func TestHandler_ReviewFlow(t *testing.T) {
	s := newSite(t)
	first := s.api("POST", "api/start", "{}", 200)
	if first["left"].(float64) != 2 {
		t.Fatalf("start: %v", first)
	}
	card := first["card"].(map[string]any)
	n := int(card["n"].(float64))
	if n != 1 && n != 2 {
		t.Fatalf("a waiting card was shown: %v", card)
	}
	if again := s.api("GET", "api/next", "", 200); again["card"].(map[string]any)["n"].(float64) != float64(n) {
		t.Errorf("next changed the card before a rating: %v", again)
	}

	// Easy sends the card to review, so the other one comes next.
	second := s.api("POST", "api/review", `{"n":`+itoa(n)+`,"rating":4}`, 200)
	other := 3 - n
	if second["left"].(float64) != 1 || int(second["card"].(map[string]any)["n"].(float64)) != other {
		t.Fatalf("after one review: %v", second)
	}
	end := s.api("POST", "api/review", `{"n":`+itoa(other)+`,"rating":4}`, 200)
	if end["card"] != nil || end["left"].(float64) != 0 {
		t.Errorf("the sitting should be over: %v", end)
	}
	if got := strings.Count(s.file(), `"rating":4,"via":"serve"`); got != 2 {
		t.Errorf("want 2 review rows via serve, got %d:\n%s", got, s.file())
	}

	if card := s.api("GET", "api/cards", "", 200)["cards"].([]any)[1].(map[string]any); card["state"] != "review" ||
		card["due"] == nil || card["src"] != "abc@src.txt:1-2" || card["concept"] != "done" || card["origin"] != "source" {
		t.Errorf("card 2 in the list: %v", card)
	}

	for body, want := range map[string]int{
		`{"n":1,"rating":0}`: 400,
		`{"n":1,"rating":5}`: 400,
		`{"n":3,"rating":3}`: 409, // waiting
		`{"n":9,"rating":3}`: 409,
		`{"n":1,"grade":3}`:  400, // unknown field
		`[1]`:                400,
	} {
		before := s.file()
		s.api("POST", "api/review", body, want)
		if s.file() != before {
			t.Errorf("%s was refused but wrote a row", body)
		}
	}
}

func TestHandler_ChangesNeedJSONPost(t *testing.T) {
	s := newSite(t)
	before := s.file()
	for _, route := range []string{"api/start", "api/review", "api/edit", "api/hold", "api/remove"} {
		for _, method := range []string{"PUT", "DELETE", "OPTIONS", "PATCH"} {
			if rec := s.do(method, "/"+testToken+"/"+route, `{"n":1}`); rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status %d, want 405", method, route, rec.Code)
			}
		}
		if rec := s.do("GET", "/"+testToken+"/"+route, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", route, rec.Code)
		}
		// The content types a cross-site form can send.
		for _, kind := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
			req := httptest.NewRequest("POST", "http://"+testHost+"/"+testToken+"/"+route, strings.NewReader(`{"n":1,"rating":3}`))
			if kind != "" {
				req.Header.Set("Content-Type", kind)
			}
			rec := httptest.NewRecorder()
			s.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Errorf("POST %s as %q: status %d, want 415", route, kind, rec.Code)
			}
		}
	}
	req := httptest.NewRequest("POST", "http://"+testHost+"/"+testToken+"/api/hold", strings.NewReader(`{"n":1,"on":true}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("application/json with a charset: status %d", rec.Code)
	}
	if !strings.HasPrefix(s.file(), before) || strings.Count(s.file(), "\n") != strings.Count(before, "\n")+1 {
		t.Errorf("only the one accepted hold should have been written:\n%s", s.file())
	}

	big := `{"n":1,"front":"` + strings.Repeat("a", maxBody) + `","back":"b"}`
	s.api("POST", "api/edit", big, 400)
}

func TestHandler_EditHoldRemove(t *testing.T) {
	s := newSite(t)
	state := func(n int) string {
		for _, c := range s.api("GET", "api/cards", "", 200)["cards"].([]any) {
			if c := c.(map[string]any); int(c["n"].(float64)) == n {
				return c["state"].(string) + " " + c["front"].(string) + "|" + c["back"].(string)
			}
		}
		return "absent"
	}
	if got := state(1); got != "new <b>front 1</b>|back 1" {
		t.Fatalf("card 1: %q", got)
	}

	s.api("POST", "api/edit", `{"n":1,"front":"new front","back":"new back"}`, 200)
	if got := state(1); got != "new new front|new back" {
		t.Errorf("after edit: %q", got)
	}
	for _, body := range []string{`{"n":1,"front":" ","back":"b"}`, `{"n":1,"front":"f"}`, `{"n":1,"front":"f","back":"` + strings.Repeat("é", 1001) + `"}`} {
		if out := s.api("POST", "api/edit", body, 400); !strings.Contains(out["error"].(string), "front is") && !strings.Contains(out["error"].(string), "back is") {
			t.Errorf("edit error: %v", out)
		}
	}

	s.api("POST", "api/hold", `{"n":1,"on":true}`, 200)
	if got := state(1); !strings.HasPrefix(got, "held ") {
		t.Errorf("after hold: %q", got)
	}
	if next := s.api("GET", "api/next", "", 200); next["left"].(float64) != 1 || next["card"].(map[string]any)["n"].(float64) != 2 {
		t.Errorf("a held card should leave the sitting: %v", next)
	}
	s.api("POST", "api/review", `{"n":1,"rating":3}`, 409)
	s.api("POST", "api/hold", `{"n":1,"on":false}`, 200)
	if got := state(1); !strings.HasPrefix(got, "new ") {
		t.Errorf("after release: %q", got)
	}

	s.api("POST", "api/remove", `{"n":2}`, 200)
	if got := state(2); got != "absent" {
		t.Errorf("after remove: %q", got)
	}
	for _, route := range []string{"api/remove", "api/hold", "api/edit"} {
		s.api("POST", route, `{"n":2,"front":"f","back":"b"}`, 409)
		s.api("POST", route, `{"n":9,"front":"f","back":"b"}`, 409)
	}
	if strings.Contains(s.file(), `"ev":"card","n":4`) {
		t.Errorf("the page must not add cards")
	}
}

func TestHandler_NewSittingNewOrder(t *testing.T) {
	s := newSite(t)
	firsts := map[float64]int{}
	for range 40 {
		firsts[s.api("POST", "api/start", "{}", 200)["card"].(map[string]any)["n"].(float64)]++
	}
	if len(firsts) != 2 {
		t.Errorf("40 sittings always opened with the same card: %v", firsts)
	}
}

func TestHandler_BrokenFiles(t *testing.T) {
	s := newSite(t)
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not a row\n")
	_ = f.Close()
	for _, route := range []string{"api/next", "api/cards"} {
		if out := s.api("GET", route, "", 500); !strings.Contains(out["error"].(string), "is not a card row") {
			t.Errorf("%s: %v", route, out)
		}
	}
	s.api("POST", "api/review", `{"n":1,"rating":3}`, 500)
	s.api("POST", "api/hold", `{"n":1,"on":true}`, 500)
}

func itoa(n int) string { return string(rune('0' + n)) }

func TestServeOptions_Check(t *testing.T) {
	ok := ServeOptions{Idle: time.Minute}
	for name, tc := range map[string]struct {
		opt  ServeOptions
		want string
	}{
		"defaults":           {ok, ""},
		"loopback v6":        {ServeOptions{Bind: "::1", Idle: time.Minute}, ""},
		"routable with port": {ServeOptions{Bind: "0.0.0.0", Port: 8765, Public: "localhost:8765", Idle: time.Minute}, ""},
		"routable no port":   {ServeOptions{Bind: "0.0.0.0", Idle: time.Minute}, "needs --port"},
		"lan no port":        {ServeOptions{Bind: "192.168.1.5", Idle: time.Minute}, "needs --port"},
		"hostname":           {ServeOptions{Bind: "localhost", Idle: time.Minute}, "not an IP address"},
		"bad port":           {ServeOptions{Port: 70000, Idle: time.Minute}, "not a port"},
		"negative port":      {ServeOptions{Port: -1, Idle: time.Minute}, "not a port"},
		"no idle":            {ServeOptions{}, "--idle must be more than zero"},
		"public no port":     {ServeOptions{Public: "localhost", Idle: time.Minute}, "not host:port"},
		"public no host":     {ServeOptions{Public: ":80", Idle: time.Minute}, "not host:port"},
	} {
		got := tc.opt.Check()
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// lineWriter passes the first line written to a channel.
type lineWriter struct{ line chan string }

func (w lineWriter) Write(p []byte) (int, error) {
	w.line <- strings.TrimSpace(string(p))
	return len(p), nil
}

var urlLine = regexp.MustCompile(`^http://127\.0\.0\.1:\d+/[0-9a-f]{32}/$`)

func TestServe(t *testing.T) {
	s := newSite(t)
	st := s.h.st
	tokens := map[string]bool{}
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		out := lineWriter{make(chan string, 1)}
		done := make(chan error, 1)
		go func() { done <- st.Serve(ctx, ServeOptions{Idle: time.Minute}, out) }()
		url := <-out.line
		if !urlLine.MatchString(url) {
			t.Fatalf("printed %q", url)
		}
		tokens[url[strings.LastIndex(strings.TrimSuffix(url, "/"), "/"):]] = true

		res, err := http.Get(url + "api/cards")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 200 || !bytes.Contains(body, []byte(`"front 2"`)) {
			t.Errorf("api/cards over the socket: %d %s", res.StatusCode, body)
		}
		res, err = http.Get(url[:strings.LastIndex(strings.TrimSuffix(url, "/"), "/")] + "/api/cards")
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != 404 {
			t.Errorf("without the token: %d", res.StatusCode)
		}
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	}
	if len(tokens) != 2 {
		t.Errorf("the token was the same on two starts")
	}
	if strings.Contains(s.file(), "token") {
		t.Errorf("the cards file mentions a token")
	}
}

func TestServe_StopsWhenIdleAndPrintsPublic(t *testing.T) {
	s := newSite(t)
	out := lineWriter{make(chan string, 1)}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- s.h.st.Serve(t.Context(), ServeOptions{Public: "localhost:8765", Idle: 150 * time.Millisecond}, out)
	}()
	if url := <-out.line; !strings.HasPrefix(url, "http://localhost:8765/") {
		t.Errorf("printed %q, want the --public host", url)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
		if time.Since(start) < 150*time.Millisecond {
			t.Errorf("stopped before the idle time")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not stop when idle")
	}
}

func TestServe_CannotListen(t *testing.T) {
	s := newSite(t)
	// 192.0.2.1 is a documentation address no interface here has.
	err := s.h.st.Serve(t.Context(), ServeOptions{Bind: "192.0.2.1", Port: 8765, Idle: time.Minute}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cannot listen") {
		t.Errorf("got %v", err)
	}
}

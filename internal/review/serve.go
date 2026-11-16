package review

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reithan/teach-me/internal/cards"
	"github.com/reithan/teach-me/internal/fsrs"
)

// page is the review page: one HTML file with inline CSS and JavaScript that
// loads nothing from the network (§17.5).
//
//go:embed page.html
var page []byte

// DefaultIdle is how long tmcards serve waits without a request before it
// stops.
const DefaultIdle = 30 * time.Minute

// maxBody caps a POST body. A card's front and back are at most
// cards.MaxFieldChars characters each, so this leaves room to spare.
const maxBody = 64 << 10

// ServeOptions are the flags of tmcards serve.
type ServeOptions struct {
	Bind   string        // address to listen on; "" means 127.0.0.1
	Port   int           // 0 lets the system pick
	Public string        // host:port the learner uses, "" when it is the bound one
	Idle   time.Duration // stop after this long without a request
}

// Check returns what is wrong with the options, or "".
func (o ServeOptions) Check() string {
	ip := net.ParseIP(o.bind())
	switch {
	case ip == nil:
		return fmt.Sprintf("--bind %q is not an IP address", o.Bind)
	case o.Port < 0 || o.Port > 65535:
		return fmt.Sprintf("--port %d is not a port", o.Port)
	case !ip.IsLoopback() && o.Port == 0:
		return "--bind with an address other than loopback needs --port"
	case o.Idle <= 0:
		return "--idle must be more than zero"
	}
	if o.Public != "" {
		if host, port, err := net.SplitHostPort(o.Public); err != nil || host == "" || port == "" {
			return fmt.Sprintf("--public %q is not host:port", o.Public)
		}
	}
	return ""
}

func (o ServeOptions) bind() string {
	if o.Bind == "" {
		return "127.0.0.1"
	}
	return o.Bind
}

// Serve runs the review page until no request has come for opt.Idle or ctx
// is done. It prints one line to out: the page URL, token included. The
// token is new on every call and is written nowhere else.
func (st *Store) Serve(ctx context.Context, opt ServeOptions, out io.Writer) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(opt.bind(), strconv.Itoa(opt.Port)))
	if err != nil {
		return fmt.Errorf("cannot listen: %w", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		_ = ln.Close()
		return fmt.Errorf("cannot make a token: %w", err)
	}
	token := hex.EncodeToString(raw)

	hosts := []string{net.JoinHostPort(opt.bind(), port), "localhost:" + port}
	shown := hosts[0]
	if opt.Public != "" {
		hosts = append(hosts, opt.Public)
		shown = opt.Public
	}

	idle := time.NewTimer(opt.Idle)
	defer idle.Stop()
	h := newHandler(st, token, hosts)
	h.touch = func() { idle.Reset(opt.Idle) }
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}

	failed := make(chan error, 1)
	go func() { failed <- srv.Serve(ln) }()
	say(out, "http://%s/%s/\n", shown, token)

	select {
	case err := <-failed:
		return err
	case <-idle.C:
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(stop)
	return nil
}

// handler serves the page and its JSON API under /<token>/. One mutex
// serializes every request: the store's random source and the session queue
// are not safe for concurrent use, and one learner needs no more.
type handler struct {
	st    *Store
	token string
	hosts []string
	touch func() // called on every request that carries the token

	mu      sync.Mutex
	session *Session
}

func newHandler(st *Store, token string, hosts []string) *handler {
	return &handler{st: st, token: token, hosts: hosts, touch: func() {}}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A page that rebinds a DNS name to the loopback address sends its own
	// name in Host, so it is refused here.
	if !h.hostAllowed(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/")
	got, route, found := strings.Cut(rest, "/")
	if !ok || !found || subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
		http.NotFound(w, r)
		return
	}
	h.touch()
	h.mu.Lock()
	defer h.mu.Unlock()

	head := w.Header()
	head.Set("Cache-Control", "no-store")
	head.Set("X-Content-Type-Options", "nosniff")
	// The token is in the URL, so it must not leak in a Referer header.
	head.Set("Referrer-Policy", "no-referrer")
	head.Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")

	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		switch route {
		case "":
			head.Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		case "api/next":
			h.next(w)
		case "api/cards":
			h.list(w)
		default:
			http.NotFound(w, r)
		}
		return
	}

	actions := map[string]func(http.ResponseWriter, request){
		"api/start":  h.start,
		"api/review": h.review,
		"api/edit":   h.edit,
		"api/hold":   h.hold,
		"api/remove": h.remove,
	}
	act, known := actions[route]
	switch {
	case !known:
		http.NotFound(w, r)
		return
	case r.Method != http.MethodPost:
		w.Header().Set("Allow", http.MethodPost)
		fail(w, http.StatusMethodNotAllowed, "changes need POST")
		return
	}
	// A cross-site form cannot send application/json, and a cross-site fetch
	// that tries is stopped by the browser's preflight, which gets a 405.
	if kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || kind != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "changes need Content-Type: application/json")
		return
	}
	var req request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the request body is not the JSON this route takes")
		return
	}
	act(w, req)
}

func (h *handler) hostAllowed(host string) bool {
	for _, ok := range h.hosts {
		if strings.EqualFold(host, ok) {
			return true
		}
	}
	return false
}

// request is the body of every POST; each route reads the fields it needs.
type request struct {
	N      int    `json:"n"`
	Rating int    `json:"rating"`
	Front  string `json:"front"`
	Back   string `json:"back"`
	On     bool   `json:"on"`
}

// cardJSON is a card as the page sees it.
type cardJSON struct {
	N       int    `json:"n"`
	Front   string `json:"front"`
	Back    string `json:"back"`
	Concept string `json:"concept,omitempty"`
	Origin  string `json:"origin"`
	Src     string `json:"src,omitempty"`
	State   string `json:"state"`
	Due     string `json:"due,omitempty"` // RFC 3339; absent for a card never reviewed
	Lapses  int    `json:"lapses"`
}

func (v *View) cardJSON(c *cards.Card) *cardJSON {
	out := &cardJSON{
		N: c.N, Front: c.Front, Back: c.Back, Concept: c.Concept, Origin: c.Origin, Src: c.Src,
		State: string(c.State(v.Passed)), Lapses: c.Lapses,
	}
	if c.Last != nil {
		out.Due = c.Last.Due.UTC().Format(time.RFC3339)
	}
	return out
}

// nextJSON answers api/next, api/start, and api/review: the card to show
// (null when the sitting is over) and how many are left including it.
type nextJSON struct {
	Left int       `json:"left"`
	Card *cardJSON `json:"card"`
}

func send(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, msg string) {
	send(w, status, map[string]string{"error": msg})
}

// failErr picks the status for a store error: 409 for a card that cannot
// take the change, 500 for files that cannot be read or written.
func failErr(w http.ResponseWriter, err error) {
	var nr *NotReviewableError
	if errors.As(err, &nr) {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	fail(w, http.StatusInternalServerError, err.Error())
}

// start begins a new sitting, which draws a new random order. The page
// calls it on every load.
func (h *handler) start(w http.ResponseWriter, _ request) {
	h.session = nil
	h.next(w)
}

func (h *handler) next(w http.ResponseWriter) {
	if h.session == nil {
		s, err := h.st.NewSession()
		if err != nil {
			failErr(w, err)
			return
		}
		h.session = s
	}
	c, left, err := h.session.Next()
	if err != nil {
		failErr(w, err)
		return
	}
	out := nextJSON{Left: left}
	if c != nil {
		v, err := h.st.Load()
		if err != nil {
			failErr(w, err)
			return
		}
		out.Card = v.cardJSON(c)
	}
	send(w, http.StatusOK, out)
}

func (h *handler) list(w http.ResponseWriter) {
	v, err := h.st.Load()
	if err != nil {
		failErr(w, err)
		return
	}
	all := make([]*cardJSON, 0, len(v.Deck.Cards))
	for _, c := range v.Deck.Cards {
		if !c.Removed {
			all = append(all, v.cardJSON(c))
		}
	}
	send(w, http.StatusOK, map[string]any{"cards": all})
}

func (h *handler) review(w http.ResponseWriter, req request) {
	if !fsrs.ValidRating(req.Rating) {
		fail(w, http.StatusBadRequest, "rating must be 1, 2, 3, or 4")
		return
	}
	if _, err := h.st.Rate(req.N, req.Rating, cards.ViaServe); err != nil {
		failErr(w, err)
		return
	}
	h.next(w)
}

// change appends one row about a card that exists and is not removed.
func (h *handler) change(w http.ResponseWriter, n int, row cards.Row) {
	v, err := h.st.Load()
	if err == nil {
		_, err = v.live(n)
	}
	if err == nil {
		err = h.st.append(row)
	}
	if err != nil {
		failErr(w, err)
		return
	}
	send(w, http.StatusOK, map[string]bool{"ok": true})
}

// edit rewords a card. Its review history and schedule are kept
// (decision 97).
func (h *handler) edit(w http.ResponseWriter, req request) {
	for _, f := range []struct{ name, text string }{{"front", req.Front}, {"back", req.Back}} {
		if what := cards.CheckField(f.text); what != "" {
			fail(w, http.StatusBadRequest, f.name+" "+what)
			return
		}
	}
	h.change(w, req.N, cards.Row{Ev: cards.EvEdit, N: req.N, Front: req.Front, Back: req.Back})
}

func (h *handler) hold(w http.ResponseWriter, req request) {
	h.change(w, req.N, cards.Row{Ev: cards.EvHold, N: req.N, On: req.On})
}

func (h *handler) remove(w http.ResponseWriter, req request) {
	h.change(w, req.N, cards.Row{Ev: cards.EvRemove, N: req.N})
}

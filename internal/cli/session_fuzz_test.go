package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/state"
)

// Opcodes of the learner-session language. A fuzz input is a sequence of
// (opcode, operand) byte pairs; the opcode is taken modulo numOps.
const (
	opAsk         = iota // ask C
	opProbe              // q C <cite> "probe"
	opTeach              // q C <cite> "teach" --teach --re Q
	opGap                // gap C "g"
	opAnswer             // answer Q "a"
	opConcede            // answer Q "a" --concede
	opPass               // grade Q pass "ok"
	opFail               // grade Q fail "no"
	opUnclear            // grade Q unclear "?"
	opReserve            // reserve C --reason "r"
	opActivate           // activate C
	opAskOverride        // ask C --override "o"
	opStatus             // status --concept C
	numOps
)

const (
	sessionMaxMoves = 24
	sessionRoot     = 0
	sessionGoal     = 1
)

var (
	sessionConcepts = [...]string{"root", "goal"}
	sessionCites    = [...]string{"src.txt:1-2", "src.txt:3-4", "src.txt:5-6", "src.txt:7-8"}
	sessionQID      = regexp.MustCompile(`^q[0-9]+$`)
	askLines        = regexp.MustCompile(`(?m)^q[0-9]+ \|`)
)

// move is one decoded learner action. concept indexes sessionConcepts, cite
// indexes sessionCites, and q indexes the questions created so far.
type move struct{ op, concept, cite, q int }

// moves encodes ms into the byte form FuzzSession decodes. The operand packs
// the concept into bit 0, the cite into bits 1-2 and the question into bits 3-7,
// so a fuzzer mutating one field leaves the others alone.
func moves(ms ...move) []byte {
	out := make([]byte, 0, 2*len(ms))
	for _, m := range ms {
		out = append(out, byte(m.op), byte(m.concept&1|(m.cite&3)<<1|(m.q&31)<<3))
	}
	return out
}

func mAsk(c int) move         { return move{op: opAsk, concept: c} }
func mProbe(c, cite int) move { return move{op: opProbe, concept: c, cite: cite} }
func mTeach(c, cite, q int) move {
	return move{op: opTeach, concept: c, cite: cite, q: q}
}
func mGap(c int) move      { return move{op: opGap, concept: c} }
func mAnswer(q int) move   { return move{op: opAnswer, q: q} }
func mConcede(q int) move  { return move{op: opConcede, q: q} }
func mPass(q int) move     { return move{op: opPass, q: q} }
func mFail(q int) move     { return move{op: opFail, q: q} }
func mReserve(c int) move  { return move{op: opReserve, concept: c} }
func mActivate(c int) move { return move{op: opActivate, concept: c} }
func mOverride(c int) move { return move{op: opAskOverride, concept: c} }
func mStatus(c int) move   { return move{op: opStatus, concept: c} }
func join(parts ...[]move) []move {
	var out []move
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// sessionSeeds replay the stuck states behind issues #78, #94 and #103 from a
// fresh graph, plus a gated concept and a frontier-blocked goal. Question
// indexes are zero-based in creation order (0 is q1).
func sessionSeeds() map[string][]byte {
	root, goal := sessionRoot, sessionGoal
	// #78: a two-question probe batch where q1 fails and q2 must still be
	// answerable, so the batch can resolve with a fail.
	failedBatch := []move{
		mProbe(root, 0), mProbe(root, 1),
		mAnswer(0), mFail(0), mAnswer(1), mPass(1),
	}
	// #94 and #103: a fallback probe batch (q3) is locked by a teach question
	// (q4) aimed at the failed probe q1. Once q4 passes, q3 is askable again.
	locked := join(failedBatch, []move{mProbe(root, 2), mGap(root), mTeach(root, 3, 0)})
	taught := join(locked, []move{mAnswer(3), mPass(3)})
	return map[string][]byte{
		"78-answer-rest-of-failed-batch": moves(join(failedBatch, []move{mAsk(root), mStatus(root)})...),
		"94-ask-after-teach-pass":        moves(join(taught, []move{mAsk(root), mStatus(root)})...),
		"103-reserve-locked-fallback":    moves(join(taught, []move{mReserve(root), mActivate(root), mAsk(root)})...),
		"103-reserve-during-teach":       moves(join(locked, []move{mReserve(root), mActivate(root)})...),
		// Fail the first probe batch, then the fallback batch once teaching
		// unlocks it: two failed probe batches gate the concept.
		"gated-after-two-failed-probe-batches": moves(join(
			[]move{mProbe(root, 0), mConcede(0), mProbe(root, 1), mGap(root), mTeach(root, 2, 0), mAnswer(2), mPass(2), mConcede(1)},
			[]move{mAsk(root), mStatus(root), mOverride(root), mAsk(root)})...),
		"goal-while-root-untested": moves(
			mProbe(goal, 0), mAsk(goal), mStatus(goal),
			mAnswer(0), mPass(0), mReserve(goal), mActivate(goal),
		),
		"goal-after-root-passes": moves(
			mProbe(root, 0), mAnswer(0), mPass(0), mAsk(root),
			mProbe(goal, 0), mAsk(goal), mStatus(goal), mAnswer(1), mPass(1), mAsk(goal),
		),
	}
}

// FuzzSession plays a learner session, decoded from the input bytes, against a
// fresh two-concept graph (goal depends on root) through the in-process CLI.
// Small TM_* limits make gates, fallback probes and spent teaching reachable
// within a dozen moves. It checks:
//
//   - no command exits 2 (post-write lint failure) or 3 (the harness only uses
//     known ids and valid syntax), and every nonzero exit writes to stderr;
//   - a successful q prints a question id;
//   - ask exiting 0 with no output never happens for an active frontier
//     concept that still has an unanswered question;
//   - status --concept chains "> tm ask" whenever ask would emit;
//   - after the last move, every untested frontier concept has at least one
//     command that succeeds, and every reserved concept can be activated.
func FuzzSession(f *testing.F) {
	f.Setenv("TM_PROBE_MIN", "1")
	f.Setenv("TM_PROBE_MAX", "2")
	f.Setenv("TM_TEACH_MIN", "1")
	f.Setenv("TM_TEACH_MAX", "1")
	f.Setenv("TM_MAX_FAILS", "2")
	f.Setenv("TM_MAX_TEACH", "2")
	f.Setenv("TM_MAX_STALL", "2")
	for _, data := range sessionSeeds() {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		playSession(t, data)
	})
}

// TestSession_Seeds replays every seed through the FuzzSession checker so the
// seeds run under plain go test with readable names.
func TestSession_Seeds(t *testing.T) {
	t.Setenv("TM_PROBE_MIN", "1")
	t.Setenv("TM_PROBE_MAX", "2")
	t.Setenv("TM_TEACH_MIN", "1")
	t.Setenv("TM_TEACH_MAX", "1")
	t.Setenv("TM_MAX_FAILS", "2")
	t.Setenv("TM_MAX_TEACH", "2")
	t.Setenv("TM_MAX_STALL", "2")
	for name, data := range sessionSeeds() {
		t.Run(name, func(t *testing.T) { playSession(t, data) })
	}
}

// session is one fuzz input's graph plus the log of commands run against it.
type session struct {
	t    *testing.T
	dir  string
	file string
	log  []string
}

// playSession builds a fresh graph, plays data against it, then checks
// end-of-session liveness.
func playSession(t *testing.T, data []byte) {
	t.Helper()
	s := newSession(t)
	for i := 0; i+1 < len(data) && i/2 < sessionMaxMoves; i += 2 {
		s.apply(int(data[i])%numOps, data[i+1])
	}
	if testing.Verbose() {
		t.Logf("commands:\n  %s", strings.Join(s.log, "\n  "))
	}
	s.checkLiveness()
}

// newSession creates the fixture: root (src.txt:1-5) and goal (src.txt:6-10),
// with goal depending on root, in a private directory.
func newSession(t *testing.T) *session {
	t.Helper()
	dir := t.TempDir()
	var src strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&src, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "src.txt"), []byte(src.String()), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	t.Setenv("TM_SRC_ROOT", dir)
	t.Setenv("TM_ERRORS", filepath.Join(dir, "ERRORS.jsonl"))
	s := &session{t: t, dir: dir, file: filepath.Join(dir, "g.mmd")}
	s.do("new", s.file)
	t.Setenv("TM_FILE", s.file)
	s.do("add", "root", "src.txt:1-5", "Root scope")
	s.do("add", "goal", "src.txt:6-10", "Goal scope", "--parent", `root:"needs"`)
	s.log = nil
	return s
}

// fatalf fails the test with msg followed by every command run so far.
func (s *session) fatalf(format string, args ...any) {
	s.t.Helper()
	s.t.Fatalf("%s\ncommands so far:\n  %s", fmt.Sprintf(format, args...), strings.Join(s.log, "\n  "))
}

// do runs one command and enforces the contracts that hold for every command.
func (s *session) do(args ...string) (stdout string, code int) {
	s.t.Helper()
	stdout, stderr, code := run(s.t, args...)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = fmt.Sprintf("%q", a)
	}
	s.log = append(s.log, fmt.Sprintf("tm %s -> exit %d %s", strings.Join(quoted, " "), code, strings.TrimSpace(stderr)))
	switch {
	case code == 2:
		s.fatalf("exit 2 (post-write lint failure) from tm %v: %s", args, stderr)
	case code == 3:
		s.fatalf("exit 3 (usage or unknown id) from tm %v: %s", args, stderr)
	case code != 0 && strings.TrimSpace(stderr) == "":
		s.fatalf("exit %d with empty stderr from tm %v", code, args)
	}
	return stdout, code
}

// apply decodes and runs one move, then its per-move contracts.
func (s *session) apply(op int, operand byte) {
	s.t.Helper()
	c := sessionConcepts[operand&1]
	cite := sessionCites[(operand>>1)&3]
	qid := ""
	if op == opTeach || (op >= opAnswer && op <= opUnclear) {
		qid = s.pickQ(operand)
	}
	switch op {
	case opAsk, opProbe, opTeach, opGap, opAskOverride:
		// ask, q and gap answer a reserved concept with exit 3 "unknown
		// concept" by design, so those moves are skipped until it is activated.
		if s.reserved(c) {
			return
		}
	}
	switch op {
	case opAsk:
		out, code := s.do("ask", c)
		s.checkAskSilence(c, out, code)
	case opProbe:
		s.checkQOutput(s.do("q", c, cite, "probe"))
	case opTeach:
		if qid != "" {
			s.checkQOutput(s.do("q", c, cite, "teach", "--teach", "--re", qid))
		}
	case opGap:
		s.do("gap", c, "g")
	case opAnswer:
		if qid != "" {
			s.do("answer", qid, "a")
		}
	case opConcede:
		if qid != "" {
			s.do("answer", qid, "a", "--concede")
		}
	case opPass:
		if qid != "" {
			s.do("grade", qid, "pass", "ok")
		}
	case opFail:
		if qid != "" {
			s.do("grade", qid, "fail", "no")
		}
	case opUnclear:
		if qid != "" {
			s.do("grade", qid, "unclear", "?")
		}
	case opReserve:
		s.do("reserve", c, "--reason", "r")
	case opActivate:
		s.do("activate", c)
	case opAskOverride:
		out, code := s.do("ask", c, "--override", "o")
		s.checkAskSilence(c, out, code)
	case opStatus:
		status, _ := s.do("status", "--concept", c)
		if s.reserved(c) {
			return
		}
		out, code := s.do("ask", c)
		s.checkAskSilence(c, out, code)
		// Only question lines count as ask emitting: with every question of an
		// open batch answered and awaiting a grade, ask prints the batch class
		// alone while status chains nothing.
		if code == 0 && askLines.MatchString(out) && !strings.Contains(status, "> tm ask "+c+"\n") {
			s.fatalf("ask %s emits a batch but status --concept %s has no chain line:\n%s", c, c, status)
		}
	}
}

// checkQOutput verifies that a successful q printed a question id.
func (s *session) checkQOutput(stdout string, code int) {
	s.t.Helper()
	if code != 0 {
		return
	}
	id := strings.TrimSpace(stdout)
	if !sessionQID.MatchString(id) {
		s.fatalf("q exited 0 but printed %q instead of a question id", stdout)
	}
}

// load reads the session graph and derived state with the current TM_* knobs.
func (s *session) load() *state.State {
	s.t.Helper()
	st, err := state.Load(os.Getenv("TM_FILE"), state.ConfigFromEnv())
	if err != nil {
		s.fatalf("load graph: %v", err)
	}
	return st
}

// answerClasses maps each answered question id to its answer class.
func answerClasses(st *state.State) map[string]string {
	out := map[string]string{}
	for _, item := range st.Graph().TestingItems {
		if item.A != nil {
			out[fmt.Sprintf("q%d", graph.QuestionN(item.A.ID))] = item.A.Class
		}
	}
	return out
}

// conceptQuestions lists the questions that resolve to concept id, in file order.
func conceptQuestions(st *state.State, id string) []*graph.QuestionNode {
	var out []*graph.QuestionNode
	for _, item := range st.Graph().TestingItems {
		if item.Q == nil {
			continue
		}
		if c, ok := st.ConceptOf(item.Q.ID); ok && c == id {
			out = append(out, item.Q)
		}
	}
	return out
}

// onFrontier reports whether id is an untested concept on the frontier.
func onFrontier(st *state.State, id string) bool {
	for _, f := range st.Frontier() {
		if f == id {
			return true
		}
	}
	return false
}

// checkAskSilence fails when ask exited 0 with no output although the concept
// is active, on the frontier, and still has an unanswered question.
func (s *session) checkAskSilence(c, stdout string, code int) {
	s.t.Helper()
	if code != 0 || strings.TrimSpace(stdout) != "" {
		return
	}
	st := s.load()
	if !onFrontier(st, c) {
		return
	}
	answers := answerClasses(st)
	for _, q := range conceptQuestions(st, c) {
		if _, ok := answers[q.ID]; !ok {
			s.fatalf("ask %s exited 0 with no output but %s is unanswered\n%s", c, q.ID, s.describe(st, c))
			return
		}
	}
}

// describe lists a concept's batches with their states, for failure messages.
func (s *session) describe(st *state.State, c string) string {
	names := map[state.BatchStatus]string{
		state.BatchDraft: "draft", state.BatchLocked: "locked",
		state.BatchOpen: "open", state.BatchResolved: "resolved",
	}
	answers := answerClasses(st)
	var b strings.Builder
	fmt.Fprintf(&b, "concept %s batches:", c)
	for _, class := range st.ConceptBatches(c) {
		fmt.Fprintf(&b, "\n  %s %s:", class, names[st.BatchStateOf(class)])
		for _, q := range st.BatchQuestions(class) {
			cls, ok := answers[q.ID]
			if !ok {
				cls = "unanswered"
			}
			fmt.Fprintf(&b, " %s=%s", q.ID, cls)
		}
	}
	return b.String()
}

// candidate is a short command sequence that should advance a concept. It
// succeeds when every step exits 0 and, if wantOutput is set, the last step
// prints something.
type candidate struct {
	name       string
	steps      [][]string
	wantOutput bool
}

// candidates lists the moves a learner could try on frontier concept c, cheapest
// first.
func candidates(st *state.State, c string) []candidate {
	answers := answerClasses(st)
	cite := sessionCites[0]
	out := []candidate{
		{"ask", [][]string{{"ask", c}}, true},
		{"q probe", [][]string{{"q", c, cite, "probe"}}, false},
	}
	qs := conceptQuestions(st, c)
	for _, q := range qs {
		switch answers[q.ID] {
		case "fail":
			teach := []string{"q", c, cite, "teach", "--teach", "--re", q.ID}
			out = append(out,
				candidate{"q teach --re " + q.ID, [][]string{teach}, false},
				candidate{"gap then q teach --re " + q.ID, [][]string{{"gap", c, "g"}, teach}, false})
		case "unclear":
			out = append(out, candidate{"q probe --re " + q.ID, [][]string{{"q", c, cite, "probe", "--re", q.ID}}, false})
		}
	}
	for _, q := range qs {
		cls, ok := answers[q.ID]
		switch {
		case !ok:
			out = append(out, candidate{"answer " + q.ID, [][]string{{"answer", q.ID, "a"}}, false})
		case cls == "pending":
			out = append(out, candidate{"grade " + q.ID + " pass", [][]string{{"grade", q.ID, "pass", "ok"}}, false})
		}
	}
	return append(out,
		candidate{"reserve", [][]string{{"reserve", c, "--reason", "r"}}, false},
		candidate{"ask --override", [][]string{{"ask", c, "--override", "o"}}, true})
}

// trial runs cand against a private copy of the graph and journal. It returns
// whether cand succeeded and, when not, a one-line reason.
func (s *session) trial(cand candidate) (bool, string) {
	s.t.Helper()
	copyDir := s.t.TempDir()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		s.fatalf("read session dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "g.mmd") || strings.HasSuffix(name, ".lock") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			s.fatalf("copy %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(copyDir, name), data, 0o644); err != nil {
			s.fatalf("copy %s: %v", name, err)
		}
	}
	s.t.Setenv("TM_FILE", filepath.Join(copyDir, "g.mmd"))
	defer s.t.Setenv("TM_FILE", s.file)
	for i, step := range cand.steps {
		stdout, stderr, code := run(s.t, step...)
		if code != 0 {
			return false, fmt.Sprintf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		if i == len(cand.steps)-1 && cand.wantOutput && strings.TrimSpace(stdout) == "" {
			return false, "exit 0 with no output"
		}
	}
	return true, ""
}

// checkLiveness verifies that every frontier concept has a command that
// advances it and every reserved concept can be activated.
func (s *session) checkLiveness() {
	s.t.Helper()
	st := s.load()
	for _, c := range st.Frontier() {
		var refusals []string
		stuck := true
		for _, cand := range candidates(st, c) {
			ok, why := s.trial(cand)
			if ok {
				stuck = false
				break
			}
			refusals = append(refusals, fmt.Sprintf("%s: %s", cand.name, why))
		}
		if stuck {
			s.fatalf("no command advances frontier concept %s\n%s\nrefused:\n    %s",
				c, s.describe(st, c), strings.Join(refusals, "\n    "))
		}
	}
	for _, c := range st.Reserve() {
		if ok, why := s.trial(candidate{"activate", [][]string{{"activate", c}}, false}); !ok {
			s.fatalf("reserved concept %s cannot be activated: %s", c, why)
		}
	}
}

// reserved reports whether concept c is currently in the reserve block.
func (s *session) reserved(c string) bool {
	s.t.Helper()
	for _, r := range s.load().Reserve() {
		if r == c {
			return true
		}
	}
	return false
}

// pickQ selects a question that is still in the graph, using the operand's
// question bits, or "" when none is. Questions of a passed concept leave the
// graph, so ids seen earlier may no longer exist.
func (s *session) pickQ(operand byte) string {
	s.t.Helper()
	var ids []string
	for _, item := range s.load().Graph().TestingItems {
		if item.Q != nil {
			ids = append(ids, item.Q.ID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return ids[int(operand>>3)%len(ids)]
}

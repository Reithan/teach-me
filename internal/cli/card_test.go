package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cardGraph has one concept in each block: done (passed), todo (untested),
// and parked (reserve).
func cardGraph() string {
	return qFrontmatter + `flowchart TB
    subgraph passed["Concepts User understands"]
        done["Done concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph untested["Concepts User has not been tested on"]
        %% tm:format 3
        todo["Todo concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph reserve["Concepts held in reserve"]
        parked["Parked concept<br/>f5ca3875b379@src.txt:1-5"]
    end
    subgraph testing["Open tests validating and teaching User understanding"]
    end
    classDef pass stroke:#3fb950
    classDef fail stroke:#f85149
    classDef unclear stroke:#d29922
    classDef pending stroke-dasharray:4 3
`
}

// cardSetup writes cardGraph and returns the directory and graph path.
func cardSetup(t *testing.T) (dir, file string) {
	t.Helper()
	dir, _ = qSetupDir(t)
	return dir, qWriteGraph(t, dir, cardGraph())
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// mustRun runs a command and fails the test unless it exits 0.
func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, code := run(t, args...)
	if code != 0 {
		t.Fatalf("tm %s: exit %d; stderr:\n%s", strings.Join(args, " "), code, errOut)
	}
	return out
}

// wantRefusal runs a command and checks its exit code and stderr text.
func wantRefusal(t *testing.T, wantCode int, wantErr string, args ...string) {
	t.Helper()
	_, errOut, code := run(t, args...)
	if code != wantCode || !strings.Contains(errOut, wantErr) {
		t.Errorf("tm %s: want exit %d with %q, got %d:\n%s", strings.Join(args, " "), wantCode, wantErr, code, errOut)
	}
}

func TestCardAdd_WritesTheCardTheMarkerAndTheEvent(t *testing.T) {
	dir, file := cardSetup(t)

	if out := mustRun(t, "card", "add", "done", "src.txt:1-2", "What is line 1?", "line 1"); out != "1\n" {
		t.Errorf("first card: got %q, want 1", out)
	}
	if out := mustRun(t, "card", "add", "done", "src.txt:3-3", "What is line 3?", "line 3"); out != "2\n" {
		t.Errorf("second card: got %q, want 2", out)
	}

	rows := strings.Split(strings.TrimSpace(readFile(t, file+".cards.jsonl")), "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 card rows, got %d:\n%s", len(rows), rows)
	}
	for _, want := range []string{
		`"ev":"card","n":1,"concept":"done","origin":"source","front":"What is line 1?","back":"line 1"`,
		`@src.txt:1-2"`,
	} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("row 1 lacks %s:\n%s", want, rows[0])
		}
	}
	if n := strings.Count(blockOf(t, file, "passed"), "%% tm:cards done"); n != 1 {
		t.Errorf("want one tm:cards line on the concept, got %d:\n%s", n, blockOf(t, file, "passed"))
	}
	log := readFile(t, file+".jsonl")
	if !strings.Contains(log, `"ev":"card","role":null,"action":"add","cards":[1],"concept":"done"`) {
		t.Errorf("event log lacks the card event:\n%s", log)
	}
	if strings.Contains(log, "What is line 1?") {
		t.Errorf("card text must stay out of the event log:\n%s", log)
	}
	lintFile(t, file, dir)
}

func TestCardAdd_Refusals(t *testing.T) {
	_, file := cardSetup(t)
	long := strings.Repeat("x", 1001)

	wantRefusal(t, 1, "err: todo is not passed", "card", "add", "todo", "src.txt:1-2", "f", "b")
	wantRefusal(t, 1, "fix: pass it first, or attach a list with tm card import <locator> --concept todo",
		"card", "add", "todo", "src.txt:1-2", "f", "b")
	wantRefusal(t, 1, "err: parked is not passed", "card", "add", "parked", "src.txt:1-2", "f", "b")
	wantRefusal(t, 3, `unknown concept "nope"`, "card", "add", "nope", "src.txt:1-2", "f", "b")
	wantRefusal(t, 3, "err: front is empty", "card", "add", "done", "src.txt:1-2", " ", "b")
	wantRefusal(t, 3, "err: back is over 1,000 characters", "card", "add", "done", "src.txt:1-2", "f", long)
	wantRefusal(t, 3, "missing line range", "card", "add", "done", "src.txt", "f", "b")
	wantRefusal(t, 3, "missing argument", "card", "add", "done", "src.txt:1-2", "f")

	if _, err := os.Stat(file + ".cards.jsonl"); err == nil {
		t.Error("a refused card add must not create the cards file")
	}
	if strings.Contains(readFile(t, file), "tm:cards") {
		t.Error("a refused card add must not mark the concept")
	}
}

func TestCardAdd_ReadsOneTextArgumentFromStdin(t *testing.T) {
	_, file := cardSetup(t)
	out, errOut, code := runIn(t, "it's `line 2` & \"more\"\n", "card", "add", "done", "src.txt:2-2", "What is line 2?", "-")
	if code != 0 || out != "1\n" {
		t.Fatalf("exit %d, out %q; stderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(readFile(t, file+".cards.jsonl"), `"back":"it's `+"`line 2`"+` & \"more\""`) {
		t.Errorf("stdin text not stored as written:\n%s", readFile(t, file+".cards.jsonl"))
	}
}

func TestCardImport_SourceListAttachedToAnUntestedConcept(t *testing.T) {
	dir, file := cardSetup(t)
	list := "# chapter words\nder Hund\tthe dog\ndie Katze\tthe cat\n\nder Hund\tthe dog\n"
	if err := os.WriteFile(filepath.Join(dir, "words.tsv"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}

	if out := mustRun(t, "card", "import", "words.tsv", "--concept", "todo"); out != "cards 2 skipped 1\n" {
		t.Errorf("first import: got %q", out)
	}
	// A second import of the same list adds nothing and logs nothing.
	if out := mustRun(t, "card", "import", "words.tsv", "--concept", "todo"); out != "cards 0 skipped 3\n" {
		t.Errorf("second import: got %q", out)
	}
	// The same pairs with no concept are different cards.
	if out := mustRun(t, "card", "import", "words.tsv"); out != "cards 2 skipped 1\n" {
		t.Errorf("import without a concept: got %q", out)
	}

	cardsFile := readFile(t, file+".cards.jsonl")
	if n := strings.Count(cardsFile, "\n"); n != 4 {
		t.Errorf("want 4 card rows, got %d:\n%s", n, cardsFile)
	}
	for _, want := range []string{
		`"n":1,"concept":"todo","origin":"source","front":"der Hund","back":"the dog","src":"`,
		`@words.tsv:1-5"`,
		`"n":3,"concept":null,"origin":"source"`,
	} {
		if !strings.Contains(cardsFile, want) {
			t.Errorf("cards file lacks %s:\n%s", want, cardsFile)
		}
	}
	if !strings.Contains(blockOf(t, file, "untested"), "%% tm:cards todo") {
		t.Errorf("concept not marked:\n%s", blockOf(t, file, "untested"))
	}
	log := readFile(t, file+".jsonl")
	if strings.Count(log, `"ev":"card"`) != 2 ||
		!strings.Contains(log, `"action":"import","cards":[1,2],"concept":"todo","origin":"source","src":"words.tsv"`) ||
		!strings.Contains(log, `"action":"import","cards":[3,4],"concept":null,"origin":"source","src":"words.tsv"`) {
		t.Errorf("event log:\n%s", log)
	}
	// A card attached to an untested concept waits.
	if out := mustRun(t, "card", "show", "1"); !strings.Contains(out, "state waiting\n") {
		t.Errorf("card 1 should wait:\n%s", out)
	}
	lintFile(t, file, dir)
}

func TestCardImport_UserAndTeacherListsAreNotSources(t *testing.T) {
	dir, file := cardSetup(t)

	out, errOut, code := runIn(t, "capital of France\tParis\n", "card", "import", "-", "--origin", "user")
	if code != 0 || out != "cards 1 skipped 0\n" {
		t.Fatalf("stdin import: exit %d, out %q; stderr:\n%s", code, out, errOut)
	}
	// A teacher list may sit outside the source root.
	outside := filepath.Join(t.TempDir(), "mine.tsv")
	if err := os.WriteFile(outside, []byte("2+2\t4\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := mustRun(t, "card", "import", outside, "--origin", "teacher", "--concept", "parked"); out != "cards 1 skipped 0\n" {
		t.Errorf("teacher import: got %q", out)
	}

	cardsFile := readFile(t, file+".cards.jsonl")
	for _, want := range []string{
		`"n":1,"concept":null,"origin":"user","front":"capital of France","back":"Paris","src":null`,
		`"n":2,"concept":"parked","origin":"teacher","front":"2+2","back":"4","src":null`,
	} {
		if !strings.Contains(cardsFile, want) {
			t.Errorf("cards file lacks %s:\n%s", want, cardsFile)
		}
	}
	if !strings.Contains(blockOf(t, file, "reserve"), "%% tm:cards parked") {
		t.Errorf("reserve concept not marked:\n%s", blockOf(t, file, "reserve"))
	}
	lintFile(t, file, dir)
}

func TestCardImport_Refusals(t *testing.T) {
	dir, file := cardSetup(t)
	if err := os.WriteFile(filepath.Join(dir, "bad.tsv"), []byte("a\tb\nno tab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.tsv"), []byte("a\tb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wantRefusal(t, 3, "err: line 2: no tab between front and back", "card", "import", "bad.tsv")
	wantRefusal(t, 3, `unknown concept "nope"`, "card", "import", "ok.tsv", "--concept", "nope")
	wantRefusal(t, 3, "--origin must be one of: source|user|teacher", "card", "import", "ok.tsv", "--origin", "web")
	wantRefusal(t, 3, "stdin is not a source", "card", "import", "-")
	wantRefusal(t, 3, "cannot read", "card", "import", "missing.tsv", "--origin", "user")
	if _, errOut, code := run(t, "card", "import", "missing.tsv"); code == 0 {
		t.Errorf("a missing source list must refuse; stderr:\n%s", errOut)
	}
	if _, err := os.Stat(file + ".cards.jsonl"); err == nil {
		t.Error("a refused import must write nothing")
	}
}

func TestCardShowEditRm(t *testing.T) {
	_, file := cardSetup(t)
	mustRun(t, "card", "add", "done", "src.txt:1-2", "front one", "back one")
	if err := os.WriteFile("loose.tsv", []byte("loose front\tloose back\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "card", "import", "loose.tsv", "--origin", "teacher")

	out := mustRun(t, "card", "show", "1")
	for _, want := range []string{"card 1\n", "state new\n", "origin source\n", "concept done\n", "@src.txt:1-2\n", "front front one\n", "back back one\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("card show 1 lacks %q:\n%s", want, out)
		}
	}
	if out := mustRun(t, "card", "show", "2"); !strings.Contains(out, "origin teacher\nconcept -\nsrc -\n") {
		t.Errorf("card show 2:\n%s", out)
	}

	if out := mustRun(t, "card", "edit", "1", "front reworded", "back reworded"); out != "ok\n" {
		t.Errorf("card edit: got %q", out)
	}
	if out := mustRun(t, "card", "show", "1"); !strings.Contains(out, "front front reworded\nback back reworded\n") {
		t.Errorf("edit not shown:\n%s", out)
	}
	if out := mustRun(t, "card", "rm", "2"); out != "ok\n" {
		t.Errorf("card rm: got %q", out)
	}

	cardsFile := readFile(t, file+".cards.jsonl")
	if !strings.Contains(cardsFile, `"ev":"edit","n":1,"front":"front reworded","back":"back reworded"`) ||
		!strings.Contains(cardsFile, `"ev":"rm","n":2}`) {
		t.Errorf("cards file:\n%s", cardsFile)
	}
	log := readFile(t, file+".jsonl")
	if !strings.Contains(log, `"action":"edit","cards":[1],"concept":"done"`) ||
		!strings.Contains(log, `"action":"rm","cards":[2],"concept":null`) {
		t.Errorf("event log:\n%s", log)
	}

	wantRefusal(t, 3, "err: card 2 was removed", "card", "show", "2")
	wantRefusal(t, 3, "err: card 2 was removed", "card", "rm", "2")
	wantRefusal(t, 3, "err: card 2 was removed", "card", "edit", "2", "f", "b")
	wantRefusal(t, 3, "err: unknown card 9", "card", "show", "9")
	wantRefusal(t, 3, `"x" is not a card number`, "card", "rm", "x")
	wantRefusal(t, 3, `"0" is not a card number`, "card", "show", "0")
	wantRefusal(t, 3, "err: back is empty", "card", "edit", "1", "f", "")
	// The removed card's number is not reused.
	if out := mustRun(t, "card", "add", "done", "src.txt:1-2", "front three", "back three"); out != "3\n" {
		t.Errorf("number after a removal: got %q, want 3", out)
	}
}

func TestCard_ABrokenCardsFileStopsEveryCardCommand(t *testing.T) {
	_, file := cardSetup(t)
	if err := os.WriteFile(file+".cards.jsonl", []byte("not a row\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("ok.tsv", []byte("a\tb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const want = "err: g.mmd.cards.jsonl line 1 is not a card row"
	wantRefusal(t, 2, want, "card", "add", "done", "src.txt:1-2", "f", "b")
	wantRefusal(t, 2, want, "card", "import", "ok.tsv")
	wantRefusal(t, 2, want, "card", "show", "1")
	wantRefusal(t, 2, want, "card", "edit", "1", "f", "b")
	wantRefusal(t, 2, want, "card", "rm", "1")
	if got := readFile(t, file+".cards.jsonl"); got != "not a row\n" {
		t.Errorf("the broken file was written to:\n%s", got)
	}
}

func TestCard_GroupHelpAndUnknownSubcommand(t *testing.T) {
	qSetupDir(t)
	const addLine = `tm card add <concept> <cite> "<front>" "<back>"` + "\n"
	for _, args := range [][]string{{"--help", "card"}, {"card", "--help"}} {
		out := mustRun(t, args...)
		if !strings.Contains(out, addLine) ||
			!strings.Contains(out, "tm card import <locator> [--concept <id>] [--origin source|user|teacher]\n") ||
			!strings.Contains(out, "tm card rm <n>\n") {
			t.Errorf("tm %v:\n%s", args, out)
		}
	}
	for _, args := range [][]string{{"--help", "card", "add"}, {"card", "add", "--help"}} {
		if out := mustRun(t, args...); out != addLine {
			t.Errorf("tm %v: got %q", args, out)
		}
	}
	if out := mustRun(t, "--help", "card", "import", "--origin"); out != "--origin source|user|teacher\n" {
		t.Errorf("flag help: got %q", out)
	}
	if out := mustRun(t, "--help", "--all"); !strings.Contains(out, addLine) {
		t.Errorf("the command list lacks the card commands:\n%s", out)
	}
	wantRefusal(t, 3, "err: card needs one of: add, import, show, edit, rm", "card")
	wantRefusal(t, 3, "fix: tm --help card", "card", "bogus")
}

func TestCard_GraderMayNotUseCardCommands(t *testing.T) {
	cardSetup(t)
	t.Setenv("TM_ROLE", "grader")
	for _, args := range [][]string{
		{"card", "add", "done", "src.txt:1-2", "f", "b"},
		{"card", "show", "1"},
	} {
		if _, _, code := run(t, args...); code == 0 {
			t.Errorf("tm %v: a grader must be refused", args)
		}
	}
}

func TestCard_StdinTextIsLoggedAsItsLength(t *testing.T) {
	_, errPath := qSetupDir(t)
	dir, _ := os.Getwd()
	qWriteGraph(t, dir, cardGraph())
	// The back comes from stdin and the concept is unknown, so the command
	// fails and ERRORS.jsonl records the argv with "-<bytes>" in its place.
	if _, _, code := runIn(t, "12345", "card", "add", "nope", "src.txt:1-2", "front", "-"); code != 3 {
		t.Fatalf("want exit 3, got %d", code)
	}
	if log := readFile(t, errPath); !strings.Contains(log, `["tm","card","add","nope","src.txt:1-2","front","-5"]`) {
		t.Errorf("ERRORS.jsonl argv:\n%s", log)
	}
}

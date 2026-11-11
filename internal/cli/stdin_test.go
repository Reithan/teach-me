package cli_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/cli"
)

// runIn runs the CLI with stdin set to in.
func runIn(t *testing.T, in string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.RunWithIO(args, strings.NewReader(in), &out, &errOut)
	return out.String(), errOut.String(), code
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestAnswer_DashReadsStdin(t *testing.T) {
	dir, _ := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	out, errOut, code := runIn(t, "left knee 90 deg\n", "answer", "q1", "-")
	if code != 0 {
		t.Fatalf("want exit 0, got %d; stderr:\n%s", code, errOut)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "left knee 90 deg") {
		t.Errorf("graph lacks the piped answer:\n%s", data)
	}
	lintFile(t, file, dir)
}

func TestAnswer_EmptyStdin_Exit3(t *testing.T) {
	for _, in := range []string{"", "\n", " \t\r\n"} {
		dir, errPath := qSetupDir(t)
		file := qWriteGraph(t, dir, answerProbeGraph())
		before, _ := os.ReadFile(file)

		_, errOut, code := runIn(t, in, "answer", "q1", "-")
		if code != 3 {
			t.Fatalf("stdin %q: want exit 3, got %d; stderr:\n%s", in, code, errOut)
		}
		if !strings.Contains(errOut, "err: answer is empty") {
			t.Errorf("stdin %q: want empty-answer err; got:\n%s", in, errOut)
		}
		if !strings.Contains(errOut, "fix: tm answer <qid>") {
			t.Errorf("stdin %q: want usage fix; got:\n%s", in, errOut)
		}
		after, _ := os.ReadFile(file)
		if !bytes.Equal(before, after) {
			t.Errorf("stdin %q: graph changed on refusal", in)
		}
		rows := readErrlog(t, errPath)
		if len(rows) != 1 || rows[0].Exit != 3 {
			t.Errorf("stdin %q: ERRORS.jsonl: want 1 row exit 3; got %v", in, rows)
		}
	}
}

func TestAnswer_BlankArgument_Exit3(t *testing.T) {
	dir, _ := qSetupDir(t)
	qWriteGraph(t, dir, answerProbeGraph())

	_, errOut, code := run(t, "answer", "q1", "   ")
	if code != 3 || !strings.Contains(errOut, "err: answer is empty") {
		t.Fatalf("want exit 3 with empty-answer err, got %d; stderr:\n%s", code, errOut)
	}
}

func TestStdin_SecondDash_Exit3(t *testing.T) {
	// No command in the table takes two stdin arguments today; the guard
	// protects a future one, since stdin can feed only one.
	cmd := &cli.Command{PosArgs: []cli.PosArg{{Name: "<a>", Stdin: true}, {Name: "<b>", Stdin: true}}}
	pos := []string{"-", "-"}
	if _, _, msg := cli.ReadStdinArgs(cmd, pos, strings.NewReader("text")); msg != "only one argument may be -" {
		t.Fatalf("want one-dash err, got %q", msg)
	}
}

func TestStdin_ReadError_Exit3(t *testing.T) {
	dir, _ := qSetupDir(t)
	qWriteGraph(t, dir, answerProbeGraph())

	var out, errOut bytes.Buffer
	code := cli.RunWithIO([]string{"answer", "q1", "-"}, failingReader{}, &out, &errOut)
	if code != 3 || !strings.Contains(errOut.String(), "err: cannot read stdin: boom") {
		t.Fatalf("want exit 3 with read err, got %d; stderr:\n%s", code, errOut.String())
	}
}

func TestStdin_DashOnNonStdinArgIsLiteral(t *testing.T) {
	dir, _ := qSetupDir(t)
	qWriteGraph(t, dir, answerProbeGraph())

	// <qid> does not accept stdin, so "-" stays an unknown question ID.
	_, errOut, code := runIn(t, "q1", "answer", "-", "text")
	if code != 3 || !strings.Contains(errOut, `err: unknown question "-"`) {
		t.Fatalf("want unknown question err, got %d; stderr:\n%s", code, errOut)
	}
}

func TestStdin_ErrlogRecordsDashAndLength(t *testing.T) {
	dir, errPath := qSetupDir(t)
	file := qWriteGraph(t, dir, answerProbeGraph())

	// q99 is unknown, so the command fails after stdin was read. The flags
	// before the positionals check that the right token is rewritten.
	_, errOut, code := runIn(t, "four\n", "answer", "--file", file, "--asked", "-", "q99", "-")
	if code != 3 {
		t.Fatalf("want exit 3, got %d; stderr:\n%s", code, errOut)
	}
	rows := readErrlog(t, errPath)
	if len(rows) != 1 {
		t.Fatalf("ERRORS.jsonl: want 1 row; got %v", rows)
	}
	want := []string{"tm", "answer", "--file", file, "--asked", "-", "q99", "-4"}
	if strings.Join(rows[0].Argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv: want %q, got %q", want, rows[0].Argv)
	}
}

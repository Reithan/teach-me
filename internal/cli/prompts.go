package cli

import (
	"embed"
	"io"
	"text/template"
)

//go:embed prompts/*.txt
var promptFS embed.FS

var promptTmpl = template.Must(
	template.New("").Option("missingkey=error").ParseFS(promptFS, "prompts/*.txt"),
)

// renderPrompt executes the named template (e.g. "check.txt") with data into w.
func renderPrompt(w io.Writer, name string, data any) error {
	return promptTmpl.ExecuteTemplate(w, name, data)
}

// checkData holds fields for the tm-check grading payload (check.txt).
type checkData struct {
	Concept  *checkConceptData
	QID      string
	Question string
	Asked    string
	Cite     string
	SrcLines []string
	IsTeach  bool
	Target   *checkTargetData
	GAP      string
	Answer   string
}

type checkConceptData struct {
	ID    string
	Scope string
}

type checkTargetData struct {
	QID   string
	Scope string
	Cite  string
}

// checkDriftData holds fields for the tm-check-drift recheck payload (check_drift.txt).
type checkDriftData struct {
	Concept string
	Grades  []driftGradeEntry
}

type driftGradeEntry struct {
	QID          string
	Scope        string
	CiteStr      string
	Drifted      bool
	GradedLines  []string
	CurrentLines []string
	Raw          string
	Verdict      string
}

// checkErrataData holds fields for the tm-check-errata recheck payload (check_errata.txt).
type checkErrataData struct {
	Concept     string
	ScopeBefore string
	ScopeAfter  string
	Reason      string
	Grades      []errataGradeEntry
}

type errataGradeEntry struct {
	QID      string
	Scope    string
	CiteStr  string
	SrcLines []string
	Raw      string
	Verdict  string
}

// frontmatterData holds fields for the tm-new YAML frontmatter block (frontmatter.txt).
type frontmatterData struct {
	Title string // already YAML-escaped by yamlStringScalar; empty if no title
}

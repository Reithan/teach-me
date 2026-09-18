package cli

// Table is the single declarative command table. Every §6 command is listed
// here; the dispatcher, help renderer, and role guard all derive their
// behavior from this slice. No command logic is scattered elsewhere.
//
// Run is nil for every command not yet implemented in this build. The
// dispatcher emits "err: <name> is not implemented in this build" (exit 3)
// for those. Later sub-PRs fill in Run without touching the table structure.
var Table = []Command{
	{
		Name: "new",
		PosArgs: []PosArg{
			{Name: "<file>"},
		},
		Flags: []FlagSpec{
			{Name: "title", TakesValue: true, ValueName: `"<t>"`},
		},
		ForbidGrader: true,
		Run:          newRun,
	},
	{
		Name: "load",
		PosArgs: []PosArg{
			{Name: "<file>"},
		},
		ForbidGrader: true,
		Run:          loadRun,
	},
	{
		Name:    "status",
		PosArgs: []PosArg{},
		Flags: []FlagSpec{
			{Name: "passed"},
			{Name: "concept", TakesValue: true, ValueName: "<id>"},
		},
		ForbidGrader: true,
		Run:          statusRun,
	},
	{
		Name: "find",
		PosArgs: []PosArg{
			{Name: `"<text>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "kind", TakesValue: true, ValueName: "concept|q|a", Values: []string{"concept", "q", "a"}},
		},
		ForbidGrader: true,
		Run:          findRun,
	},
	{
		// show is exempt from the grader ban (§7).
		Name: "show",
		PosArgs: []PosArg{
			{Name: "<id>"},
		},
		Flags: []FlagSpec{
			{Name: "history"},
		},
		ForbidGrader: false,
		Run:          showRun,
	},
	{
		Name: "add",
		PosArgs: []PosArg{
			{Name: "<id>"},
			{Name: "<cite>"},
			{Name: `"<scope>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "parent", TakesValue: true, ValueName: `<id>:"<rel>"`, Repeatable: true},
			{Name: "child", TakesValue: true, ValueName: `<id>:"<rel>"`, Repeatable: true},
		},
		ForbidGrader: true,
		Run:          addRun,
	},
	{
		Name: "link",
		PosArgs: []PosArg{
			{Name: "<from>"},
			{Name: "<to>"},
			{Name: `"<rel>"`, Stdin: true},
		},
		ForbidGrader: true,
		Run:          linkRun,
	},
	{
		Name: "edit",
		PosArgs: []PosArg{
			{Name: "<concept>"},
			{Name: `"<scope>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "src", TakesValue: true, ValueName: "<cite>"},
		},
		ForbidGrader: true,
		Run:          editRun,
	},
	{
		Name: "drop",
		PosArgs: []PosArg{
			{Name: "<concept>"},
		},
		ForbidGrader: true,
		Run:          dropRun,
	},
	{
		Name: "gap",
		PosArgs: []PosArg{
			{Name: "<concept>"},
			{Name: `"<gap>"`, Stdin: true},
		},
		ForbidGrader: true,
		Run:          gapRun,
	},
	{
		Name: "reopen",
		PosArgs: []PosArg{
			{Name: "<concept>"},
			{Name: `"<gap>"`, Stdin: true},
		},
		ForbidGrader: true,
		Run:          reopenRun,
	},
	{
		Name: "q",
		PosArgs: []PosArg{
			{Name: "<concept>"},
			{Name: "<cite>"},
			{Name: `"<narrow scope>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "re", TakesValue: true, ValueName: "<qid>"},
			{Name: "teach"},
			{Name: "override", TakesValue: true, ValueName: "<reason>"}, // --override gate-clearing wired in m6e
		},
		ForbidGrader: true,
		Run:          qRun,
	},
	{
		Name: "ask",
		PosArgs: []PosArg{
			{Name: "<concept>"},
		},
		Flags: []FlagSpec{
			{Name: "format", TakesValue: true, ValueName: "lines|json", Values: []string{"lines", "json"}},
			{Name: "src-text"},
		},
		ForbidGrader: true,
		Run:          askRun,
	},
	{
		Name: "answer",
		PosArgs: []PosArg{
			{Name: "<qid>"},
			{Name: `"<raw>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "asked", TakesValue: true, ValueName: `"<wording>"`},
			{Name: "override", TakesValue: true, ValueName: `"<reason>"`}, // gate-clearing wired in m6e
		},
		ForbidGrader: true,
		Run:          answerRun,
	},
	{
		// check is exempt from the grader ban (§7).
		Name: "check",
		PosArgs: []PosArg{
			{Name: "<qid>"},
		},
		ForbidGrader: false,
		Run:          checkRun,
	},
	{
		// grade is exempt from the grader ban but refuses TM_ROLE=teacher (§7).
		Name: "grade",
		PosArgs: []PosArg{
			{Name: "<qid>"},
			{Name: "pass|fail|unclear", Values: []string{"pass", "fail", "unclear"}},
			{Name: `"<summary>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "guided"},
			{Name: "oos"},
		},
		ForbidGrader:  false,
		ForbidTeacher: true,
		Run:           gradeRun,
	},
	{
		Name: "lint",
		PosArgs: []PosArg{
			{Name: "[<file>]", Optional: true},
		},
		ForbidGrader: true,
		Run:          lintRun,
	},
}

// tableIndex is a name → Command lookup map built from Table at init time.
var tableIndex map[string]*Command

func init() {
	tableIndex = make(map[string]*Command, len(Table))
	for i := range Table {
		tableIndex[Table[i].Name] = &Table[i]
	}
}

// findCommand returns the Command with the given name, or nil.
func findCommand(name string) *Command {
	return tableIndex[name]
}

// FindCommand returns the Command with the given name, or nil. Exported for
// use by tests and future tooling.
func FindCommand(name string) *Command {
	return tableIndex[name]
}

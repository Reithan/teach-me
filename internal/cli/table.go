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
			{Name: "src-root", TakesValue: true, ValueName: "<dir>"},
			{Name: "local"},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // new creates the file; no existing graph to check
		Run:             newRun,
	},
	{
		Name: "load",
		PosArgs: []PosArg{
			{Name: "<file>"},
		},
		Flags: []FlagSpec{
			{Name: "src-root", TakesValue: true, ValueName: "<dir>"},
			{Name: "local"},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // load checks its positional file in loadRun; central check would wrongly test the currently configured file
		Run:             loadRun,
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
			{Name: "errata", TakesValue: true, ValueName: `"<reason>"`},
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
		Flags: []FlagSpec{
			{Name: "src", TakesValue: true, ValueName: "<cite>"},
		},
		ForbidGrader: true,
		Run:          reopenRun,
	},
	{
		Name: "reserve",
		PosArgs: []PosArg{
			{Name: "<concept>"},
		},
		ForbidGrader: true,
		Run:          reserveRun,
	},
	{
		Name: "activate",
		PosArgs: []PosArg{
			{Name: "<concept>"},
		},
		ForbidGrader: true,
		Run:          activateRun,
	},
	{
		Name: "aid",
		PosArgs: []PosArg{
			{Name: "rm|<id>"},
			{Name: "<id/path>"},
			{Name: "[<path>]", Optional: true},
		},
		ForbidGrader: true,
		Run:          aidRun,
	},
	{
		Name: "prune",
		PosArgs: []PosArg{
			{Name: "<goal>"},
		},
		Flags: []FlagSpec{
			{Name: "keep", TakesValue: true, ValueName: "N"},
		},
		ForbidGrader: true,
		Run:          pruneRun,
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
			{Name: "override", TakesValue: true, ValueName: `"<reason>"`}, // gate-clearing: ask becomes mutation under --override
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
			{Name: "concede"}, // learner-declared fail; writes fail verdict in the same mutation
		},
		ForbidGrader: true,
		Run:          answerRun,
	},
	{
		// check is exempt from the grader ban (§7).
		// `tm check <qid>` grading payload; `tm check --drift <concept>` recheck payload.
		Name: "check",
		PosArgs: []PosArg{
			{Name: "<qid>"},
		},
		Flags: []FlagSpec{
			{Name: "drift"},
			{Name: "errata"},
		},
		ForbidGrader: false,
		Run:          checkRun,
	},
	{
		// grade is exempt from the grader ban but refuses TM_ROLE=teacher (§7).
		// `tm grade <qid> pass|fail|unclear "<summary>"` — standard verdict.
		// `tm grade --drift <concept> keep|reopen "<summary>"` — recheck verdict.
		Name: "grade",
		PosArgs: []PosArg{
			{Name: "<qid>"},
			// Values expanded to accept keep|reopen for --drift path; runtime
			// validation in gradeRun rejects keep|reopen without --drift and
			// pass|fail|unclear with --drift.
			{Name: "pass|fail|unclear", Values: []string{"pass", "fail", "unclear", "keep", "reopen"}},
			{Name: `"<summary>"`, Stdin: true},
		},
		Flags: []FlagSpec{
			{Name: "guided"},
			{Name: "oos"},
			{Name: "drift"},
			{Name: "errata"},
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
		Flags: []FlagSpec{
			{Name: "drift"},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // lint diagnoses the graph; must accept any format
		Run:             lintRun,
	},
	{
		Name: "report",
		PosArgs: []PosArg{
			{Name: "[<concept>]", Optional: true},
		},
		Flags: []FlagSpec{
			{Name: "hops", TakesValue: true, ValueName: "N"},
			{Name: "fulltext"},
			{Name: "passed-only"},
			{Name: "reserve"},
		},
		ForbidGrader: true,
		Run:          reportRun,
	},
	{
		Name: "rehash",
		PosArgs: []PosArg{
			{Name: "[<file>]", Optional: true},
		},
		ForbidGrader: true,
		Run:          rehashRun,
	},
	{
		// recite re-points a concept citation to a new range that hashes to
		// the same content (text moved, unchanged). Logged as recite event.
		Name: "recite",
		PosArgs: []PosArg{
			{Name: "<concept>"},
			{Name: "<locator>:START-END"},
		},
		ForbidGrader: true,
		Run:          reciteRun,
	},
	{
		// migrate upgrades a graph from an older format to the current one (§13.2).
		Name: "migrate",
		PosArgs: []PosArg{
			{Name: "[<file>]", Optional: true},
		},
		Flags: []FlagSpec{
			{Name: "dry-run"},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // migrate upgrades the format; must accept old format
		Run:             migrateRun,
	},
	{
		// repo manages machine-local repo aliases for git: locators (§13).
		// Subcommands: add <alias> <path> [--local], rm <alias>, list.
		Name: "repo",
		PosArgs: []PosArg{
			{Name: "add|rm|list", Values: []string{"add", "rm", "list"}},
			{Name: "[<alias>]", Optional: true},
			{Name: "[<path>]", Optional: true},
		},
		Flags: []FlagSpec{
			{Name: "local"},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // repo commands do not touch a graph
		Run:             repoRun,
	},
	{
		// cache manages the conversion and fetch cache (§13.1, §3).
		// Subcommands: clear, list.
		Name: "cache",
		PosArgs: []PosArg{
			{Name: "clear|list", Values: []string{"clear", "list"}},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // cache commands do not touch a graph
		Run:             cacheRun,
	},
	{
		// src resolves a locator through the citation pipeline and prints its
		// converted text with 1-based line numbers. An optional START-END
		// positional restricts to that range; --find <regex> filters to
		// matching lines. Used to obtain stable line references for tm add.
		Name: "src",
		PosArgs: []PosArg{
			{Name: "<locator>"},
			{Name: "[START-END]", Optional: true},
		},
		Flags: []FlagSpec{
			{Name: "find", TakesValue: true, ValueName: "<regex>"},
			{Name: "fulldump"},
		},
		ForbidGrader:    true,
		SkipFormatCheck: true, // src does not parse a graph
		Run:             srcRun,
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

package graph

import (
	"regexp"
	"strings"
)

// conceptIDRe matches valid concept IDs: [a-z][a-z0-9_]*.
var conceptIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// qaIDRe matches the q/a-prefixed IDs reserved for questions and answers.
var qaIDRe = regexp.MustCompile(`^[qa][0-9]+$`)

// mermaidKeywords is the set of IDs reserved by the Mermaid language (spec 4.3).
var mermaidKeywords = map[string]bool{
	"end": true, "graph": true, "flowchart": true, "subgraph": true,
	"class": true, "classDef": true, "click": true, "style": true,
	"default": true,
}

// blockIDs is the set of IDs used for the three fixed subgraph blocks.
var blockIDs = map[string]bool{
	"passed": true, "untested": true, "testing": true,
}

// ValidConceptID reports whether s is a valid concept ID: it matches
// [a-z][a-z0-9_]*, is not a q/a-prefixed question/answer ID, is not a
// reserved block ID, and is not a Mermaid keyword.
func ValidConceptID(s string) bool {
	if !conceptIDRe.MatchString(s) {
		return false
	}
	if qaIDRe.MatchString(s) {
		return false
	}
	if blockIDs[s] {
		return false
	}
	if mermaidKeywords[s] {
		return false
	}
	return true
}

// ValidQuestionID reports whether s is a valid question ID (qN form).
func ValidQuestionID(s string) bool {
	return len(s) >= 2 && s[0] == 'q' && isDigits(s[1:])
}

// ValidAnswerID reports whether s is a valid answer ID (aN form).
func ValidAnswerID(s string) bool {
	return len(s) >= 2 && s[0] == 'a' && isDigits(s[1:])
}

// QuestionN returns the numeric suffix of a q/a-prefixed ID (e.g. "q5" → 5).
// Returns 0 if the suffix is missing or not a valid number.
func QuestionN(s string) int {
	if len(s) < 2 {
		return 0
	}
	return parseDigits(s[1:])
}

// ValidBatchID reports whether s is a valid batch class ID (probe_N or teach_N).
func ValidBatchID(s string) bool {
	return IsProbeClass(s) || IsTeachClass(s)
}

// IsProbeClass reports whether class is a probe_N batch class.
func IsProbeClass(class string) bool {
	if !strings.HasPrefix(class, "probe_") {
		return false
	}
	return isDigits(class[6:]) && len(class) > 6
}

// IsTeachClass reports whether class is a teach_N batch class.
func IsTeachClass(class string) bool {
	if !strings.HasPrefix(class, "teach_") {
		return false
	}
	return isDigits(class[6:]) && len(class) > 6
}

// BatchN returns the numeric suffix of a batch class (e.g. "probe_2" → 2).
// Returns 0 if class is not a valid batch class.
func BatchN(class string) int {
	if IsProbeClass(class) {
		return parseDigits(class[6:])
	}
	if IsTeachClass(class) {
		return parseDigits(class[6:])
	}
	return 0
}

// IsAnswerClass reports whether class is one of the four answer classes.
func IsAnswerClass(class string) bool {
	switch class {
	case "pending", "pass", "fail", "unclear":
		return true
	}
	return false
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseDigits(s string) int {
	n := 0
	for i := range len(s) {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

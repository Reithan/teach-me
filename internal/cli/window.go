package cli

// Bounds for prompt-shaped output (spec §1). These are constants, not
// configuration: the spec fixes the numbers so tm never depends on a harness
// output cap that keeps the tail and drops the head. Nothing here is exported.
const (
	// windowLineMax and windowCharMax bound one source-print window (§1):
	// tm src and tm report --fulltext print at most this many whole lines or
	// characters, whichever comes first.
	windowLineMax = 200
	windowCharMax = 8000
)

// windowLines returns the leading whole lines of lines that fit within one
// window (§1): at most windowLineMax lines and windowCharMax characters,
// counting each line plus its trailing newline. It stops before the line whose
// inclusion would exceed either bound, keeping whole lines only. A single first
// line longer than the character bound is still returned alone, so progress is
// always made.
//
// startNo is the 1-based number of lines[0]. nextStart is the number of the
// first line not kept (startNo+len(lines) when all fit), and more reports
// whether any line was dropped.
func windowLines(lines []string, startNo int) (kept []string, nextStart int, more bool) {
	chars := 0
	for i, line := range lines {
		cost := len(line) + 1 // line plus its newline
		if i > 0 && (i >= windowLineMax || chars+cost > windowCharMax) {
			return lines[:i], startNo + i, true
		}
		chars += cost
	}
	return lines, startNo + len(lines), false
}

package docver_test

import (
	"strings"
	"testing"

	"github.com/reithan/teach-me/internal/docver"
	"github.com/reithan/teach-me/internal/version"
)

func TestDocVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVal string
		wantOK  bool
	}{
		{
			name: "double-quoted value",
			input: `---
metadata:
  tm-version: "0.1"
---
body
`,
			wantVal: "0.1",
			wantOK:  true,
		},
		{
			name: "single-quoted value",
			input: `---
metadata:
  tm-version: '0.2'
---
`,
			wantVal: "0.2",
			wantOK:  true,
		},
		{
			name: "unquoted value",
			input: `---
metadata:
  tm-version: 0.3
---
`,
			wantVal: "0.3",
			wantOK:  true,
		},
		{
			name: "stray top-level tm-version must not match",
			input: `---
tm-version: "0.1"
metadata:
  other: value
---
`,
			wantVal: "",
			wantOK:  false,
		},
		{
			name:    "missing frontmatter",
			input:   "# no frontmatter\nbody\n",
			wantVal: "",
			wantOK:  false,
		},
		{
			name: "unclosed frontmatter",
			input: `---
metadata:
  tm-version: "0.1"
`,
			wantVal: "",
			wantOK:  false,
		},
		{
			name: "metadata present but no tm-version",
			input: `---
metadata:
  other: value
---
`,
			wantVal: "",
			wantOK:  false,
		},
		{
			name: "extra indented siblings under metadata",
			input: `---
metadata:
  author: "Alice"
  tm-version: "0.1"
  description: "test"
---
`,
			wantVal: "0.1",
			wantOK:  true,
		},
		{
			name:    "CRLF line endings",
			input:   "---\r\nmetadata:\r\n  tm-version: \"0.1\"\r\n---\r\n",
			wantVal: "0.1",
			wantOK:  true,
		},
		{
			name: "tm-version under different top-level key not matched",
			input: `---
other:
  tm-version: "0.1"
metadata:
  something: else
---
`,
			wantVal: "",
			wantOK:  false,
		},
		{
			name: "empty frontmatter",
			input: `---
---
`,
			wantVal: "",
			wantOK:  false,
		},
		{
			name: "leading empty lines before frontmatter",
			input: `
---
metadata:
  tm-version: "0.1"
---
`,
			wantVal: "0.1",
			wantOK:  true,
		},
		{
			name: "no metadata key at all",
			input: `---
title: my doc
version: 1
---
`,
			wantVal: "",
			wantOK:  false,
		},
		{
			name:    "tab-indented tm-version",
			input:   "---\nmetadata:\n\ttm-version: \"0.1\"\n---\n",
			wantVal: "0.1",
			wantOK:  true,
		},
		{
			// After 'other:' resets inMetadata, tm-version is NOT under metadata.
			name: "tm-version after metadata key reset by another top-level key",
			input: `---
metadata:
  sibling: x
other:
  tm-version: "0.1"
---
`,
			wantVal: "",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := docver.DocVersion([]byte(tt.input))
			if ok != tt.wantOK || got != tt.wantVal {
				t.Errorf("DocVersion() = (%q, %v), want (%q, %v)", got, ok, tt.wantVal, tt.wantOK)
			}
		})
	}
}

func TestMarker(t *testing.T) {
	m := docver.Marker()
	// Must be exactly two dot-separated components (major.minor).
	parts := strings.Split(m, ".")
	if len(parts) != 2 {
		t.Errorf("Marker() = %q, want major.minor format", m)
	}
	// Must equal the major.minor prefix of version.Version().
	want := strings.Join(strings.SplitN(version.Version(), ".", 3)[:2], ".")
	if m != want {
		t.Errorf("Marker() = %q, want %q", m, want)
	}
}

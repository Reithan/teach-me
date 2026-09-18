// Corpus writes graph corpus files for the Mermaid conformance suite.
//
// Usage:
//
//	go run ./internal/tools/corpus -out conformance/corpus
//
// It writes every golden graph from testdata/ (with a sidecar JSON), then 500
// seeded-random valid graphs (also with sidecars).  Each sidecar maps every
// node ID to the expected Mermaid subgraph name per the §4.2 first-mention
// rule.  The output directory is created if it does not exist.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/reithan/teach-me/internal/graph"
	"github.com/reithan/teach-me/internal/graph/gen"
)

const (
	numGenerated  = 500
	generatedSeed = int64(0)
)

func main() {
	outDir := flag.String("out", "conformance/corpus", "output directory for corpus files")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "corpus: mkdir %s: %v\n", *outDir, err)
		os.Exit(1)
	}

	written := 0
	errs := 0

	// Copy golden files from testdata/.
	goldens, err := filepath.Glob("testdata/*.mmd")
	if err != nil {
		fmt.Fprintf(os.Stderr, "corpus: glob testdata/*.mmd: %v\n", err)
		os.Exit(1)
	}
	for _, src := range goldens {
		data, err := os.ReadFile(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "corpus: read %s: %v\n", src, err)
			errs++
			continue
		}
		g, err := graph.Parse(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "corpus: parse %s: %v\n", src, err)
			errs++
			continue
		}

		base := filepath.Base(src)
		if err := writeCorpusEntry(*outDir, base, data, gen.BuildSidecar(g)); err != nil {
			fmt.Fprintf(os.Stderr, "corpus: write %s: %v\n", base, err)
			errs++
			continue
		}
		written += 2 // .mmd + .json
	}

	// Generate 500 graphs at a fixed seed.
	r := rand.New(rand.NewSource(generatedSeed))
	for i := range numGenerated {
		g := gen.GraphRand(r)
		data := graph.Write(g)
		base := fmt.Sprintf("gen%04d.mmd", i)
		if err := writeCorpusEntry(*outDir, base, data, gen.BuildSidecar(g)); err != nil {
			fmt.Fprintf(os.Stderr, "corpus: write %s: %v\n", base, err)
			errs++
			continue
		}
		written += 2 // .mmd + .json
	}

	fmt.Printf("corpus: wrote %d files to %s\n", written, *outDir)

	if errs > 0 {
		os.Exit(1)
	}
}

// writeCorpusEntry writes a .mmd file and its sidecar .json to outDir.
func writeCorpusEntry(outDir, mmdName string, mmdData []byte, sidecar map[string]string) error {
	mmdPath := filepath.Join(outDir, mmdName)
	if err := os.WriteFile(mmdPath, mmdData, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", mmdPath, err)
	}

	jsonName := mmdName[:len(mmdName)-len(".mmd")] + ".json"
	jsonPath := filepath.Join(outDir, jsonName)
	jsonData, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sidecar for %s: %w", mmdName, err)
	}
	jsonData = append(jsonData, '\n')
	if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", jsonPath, err)
	}

	return nil
}

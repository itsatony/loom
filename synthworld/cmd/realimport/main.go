// realimport imports REAL external data (Wikidata + Wikipedia) into the
// synthworld dataset format so the existing toolchain (cmd/validate,
// cmd/harness) runs on it unchanged. This is "Rung 0": a non-falsifying
// plumbing gate that proves the instrument accepts real-world corpora.
//
// Two-phase, network isolated from the deterministic build:
//
//	realimport -fetch -snapshot snapshot.json          # ONE live fetch -> pinned snapshot
//	realimport -snapshot snapshot.json -out dataset    # network-free deterministic build
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	fetch := flag.Bool("fetch", false, "fetch live data from Wikidata + Wikipedia and write the snapshot")
	snapshot := flag.String("snapshot", "snapshot.json", "pinned snapshot path (written by -fetch, read by the build)")
	out := flag.String("out", "", "output dataset directory (build path); leave empty when only fetching")
	flag.Parse()

	if *fetch {
		snap := fetchAll()
		if err := writeSnapshot(*snapshot, snap); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "snapshot written to %s: %d countries\n", *snapshot, len(snap.Countries))
		if *out == "" {
			return
		}
	}

	if *out == "" {
		fatal(fmt.Errorf("-out is required for the build path (no -fetch), e.g. -out /tmp/rung0"))
	}

	snap, err := readSnapshot(*snapshot)
	if err != nil {
		fatal(fmt.Errorf("read snapshot: %w", err))
	}
	if err := buildDataset(snap, *out); err != nil {
		fatal(err)
	}
	fmt.Printf("dataset written to %s\n", *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// apiimport imports a REAL open-source project's API version-history /
// deprecation record (Rung 1: Django, pinned at 6.1b1) into the synthworld
// dataset format so the existing toolchain (cmd/validate, cmd/harness,
// cmd/fidelity, cmd/memoexport) runs on it unchanged.
//
// Rung 1 is THE falsifying rung (kill criterion §7 binds here): version =
// effective date; deprecation = fact; removal = supersession (availability
// flips stale->current); Django's arithmetic deprecation POLICY = a stated
// Horn rule whose removal-version conclusion composes across disjoint passages
// (deprecation note + release calendar + policy) — the go/no-go composition
// slice. All content is dated in/after the 6.1 cycle (alpha 2026-05-20), i.e.
// after the model knowledge cutoff, so parametric leakage is scrubbed.
//
// The snapshot is a PINNED, URL-cited transcription of the real Django release
// notes; the build path is network-free and deterministic (same snapshot +
// same binary => byte-identical dataset).
//
//	apiimport -snapshot snapshot.json -out dataset
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	snapshot := flag.String("snapshot", "snapshot.json", "pinned snapshot path")
	out := flag.String("out", "", "output dataset directory")
	flag.Parse()

	if *out == "" {
		fatal(fmt.Errorf("-out is required, e.g. -out /tmp/rung1"))
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

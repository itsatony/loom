// synthgen generates a dataset: world.json (oracle-only), episodes.jsonl
// (what the system under test sees), queries.jsonl (eval items with ground
// truth), manifest.json.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vaudience/synthworld/gen"
)

func main() {
	seed := flag.Int64("seed", 42, "generation seed")
	out := flag.String("out", "dataset", "output directory")
	preset := flag.String("preset", "small", "small | medium | batch")
	flag.Parse()

	cfg, err := gen.PresetConfig(*preset, *seed)
	if err != nil {
		fatal(err)
	}

	b := gen.NewBuilder(cfg)
	if err := b.BuildWorld(); err != nil {
		fatal(fmt.Errorf("build world: %w", err))
	}
	episodes := b.BuildEpisodes()
	qs, err := b.BuildQueries()
	if err != nil {
		fatal(fmt.Errorf("build queries: %w", err))
	}
	w := b.World()

	if err := b.WriteDataset(*out, episodes, qs); err != nil {
		fatal(err)
	}

	fmt.Printf("dataset written to %s\n", *out)
	fmt.Printf("world: %d entities, %d relations, %d facts, %d rules, %d supersessions\n",
		len(w.Entities), len(w.Relations), len(w.Facts), len(w.Rules), len(w.Supersessions))
	fmt.Printf("episodes: %d, queries: %d\n", len(episodes), len(qs.Queries))
	fmt.Print(gen.QueryStats(qs))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

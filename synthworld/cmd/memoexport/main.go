// memoexport writes a dataset's episodes as tmr-ingestible *.memo.md files:
// one memo per episode. The memo ID is "mem_" + episode ID (tmr requires the
// mem_ prefix; retrieve results carry it back as memo_id, which is how the
// harness TmrRetriever maps hits to episodes — strip the prefix).
//
// Front-matter conforms to tmr's MemoFrontmatter contract (validated on
// ingest): source_kind/scope/memory_type/sensitivity/status are required
// enums; created_at is derived deterministically from the episode day
// (epoch 2025-01-01 + day), so re-export is byte-identical.
//
// Usage:
//
//	memoexport -dir dataset -out ./mymem
//	tmr init ./mymem && tmr ingest ./mymem     # needs BABYLON_EMBED_KEY
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vaudience/synthworld/gen"
)

// memoEpoch anchors day 0. Fixed forever: changing it breaks byte-identical
// re-export and invalidates tmr ingests keyed by content hash.
var memoEpoch = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	dir := flag.String("dir", "dataset", "dataset directory")
	out := flag.String("out", "memos", "output folder for *.memo.md files")
	flag.Parse()

	var episodes []gen.Episode
	f, err := os.Open(filepath.Join(*dir, "episodes.jsonl"))
	must(err)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var ep gen.Episode
		must(json.Unmarshal(sc.Bytes(), &ep))
		episodes = append(episodes, ep)
	}
	must(sc.Err())
	f.Close()

	must(os.MkdirAll(*out, 0o755))
	for _, ep := range episodes {
		created := memoEpoch.AddDate(0, 0, ep.Day).Format(time.RFC3339)
		body := fmt.Sprintf(`---
id: mem_%s
source_kind: document
source_id: %s
created_at: %s
scope: project
project: synthworld
memory_type: event
sensitivity: internal
status: active
confidence: 1.0
importance: 0.5
---

# Episode %s (day %d)

%s
`, ep.ID, ep.ID, created, ep.ID, ep.Day, ep.Text)
		must(os.WriteFile(filepath.Join(*out, ep.ID+".memo.md"), []byte(body), 0o644))
	}
	fmt.Printf("wrote %d memos to %s\n", len(episodes), *out)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

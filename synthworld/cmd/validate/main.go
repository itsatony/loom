// validate re-loads a generated dataset and independently verifies the
// guarantees in DESIGN.md §5: ground truth reproduces under the oracle,
// revision queries flip / retain as labeled, composition provenance spans
// >= 2 episodes, and no query depends on knowledge revealed after its day.
// The verification itself lives in gen.VerifyDataset, shared with cmd/batch.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/vaudience/synthworld/gen"
)

func main() {
	dir := flag.String("dir", "dataset", "dataset directory")
	flag.Parse()

	rep, err := gen.VerifyDataset(*dir)
	if err != nil {
		fail("%v", err)
	}
	for _, p := range rep.Problems {
		fmt.Println(p)
	}

	fmt.Println("checked:")
	var keys []string
	for k := range rep.Checked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-24s %d\n", k, rep.Checked[k])
	}
	if rep.OK() {
		fmt.Println("OK — all guarantees hold")
		return
	}
	fail("%d problems found", len(rep.Problems))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

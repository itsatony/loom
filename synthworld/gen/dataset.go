package gen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteDataset writes the four dataset files (world.json, episodes.jsonl,
// queries.jsonl, manifest.json) exactly as cmd/synthgen always has — this is
// the shared write path for cmd/synthgen and cmd/batch. Byte-identity per
// (seed, preset, binary) is a contract: do not reorder fields or change
// encoder settings.
func (b *Builder) WriteDataset(dir string, episodes []Episode, qs *QuerySet) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w := b.w
	if err := writeJSONFile(filepath.Join(dir, "world.json"), w); err != nil {
		return err
	}
	if err := writeJSONLFile(filepath.Join(dir, "episodes.jsonl"), func(emit func(any) error) error {
		for _, ep := range episodes {
			if err := emit(ep); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := writeJSONLFile(filepath.Join(dir, "queries.jsonl"), func(emit func(any) error) error {
		for _, q := range qs.Queries {
			if err := emit(q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	manifest := map[string]any{
		"generator":         "synthworld v0.1",
		"config":            b.cfg,
		"eval_day":          qs.AtDay,
		"num_entities":      len(w.Entities),
		"num_facts":         len(w.Facts),
		"num_rules":         len(w.Rules),
		"num_supersessions": len(w.Supersessions),
		"num_episodes":      len(episodes),
		"num_queries":       len(qs.Queries),
		"quality":           b.Stats,
		"note": "Systems under test may read episodes.jsonl ONLY. " +
			"world.json and query answers/traces are for scoring. " +
			"Episode events carry structured payloads (easy mode) and text (hard mode).",
	}
	return writeJSONFile(filepath.Join(dir, "manifest.json"), manifest)
}

func writeJSONFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return f.Close()
}

func writeJSONLFile(path string, produce func(emit func(any) error) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := produce(func(v any) error { return enc.Encode(v) }); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return f.Close()
}

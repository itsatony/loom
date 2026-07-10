package gen

import (
	"reflect"
	"testing"
)

// The scale presets derive from batch-level knobs; they must never mutate
// what "batch" itself returns (byte-identity of batch datasets is sacred:
// the locked E4 seed list was generated with it).
func TestScalePresetsDoNotAlterBatch(t *testing.T) {
	before, err := PresetConfig("batch", 42)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scale3x", "scale10x"} {
		if _, err := PresetConfig(name, 42); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	after, err := PresetConfig("batch", 42)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("batch preset changed after building scale presets:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestScalePresetKnobs(t *testing.T) {
	batch, _ := PresetConfig("batch", 7)
	s3, err := PresetConfig("scale3x", 7)
	if err != nil {
		t.Fatal(err)
	}
	s10, err := PresetConfig("scale10x", 7)
	if err != nil {
		t.Fatal(err)
	}
	// Query structure identical to batch — corpus size is the only variable.
	for name, cfg := range map[string]Config{"scale3x": s3, "scale10x": s10} {
		if cfg.NumRepetitionQueries != batch.NumRepetitionQueries ||
			cfg.NumCompositionQueries != batch.NumCompositionQueries ||
			cfg.NumFindQueries != batch.NumFindQueries ||
			cfg.NumRevisionQueries != batch.NumRevisionQueries ||
			cfg.NumRevisionPairs != batch.NumRevisionPairs {
			t.Errorf("%s: query knobs differ from batch", name)
		}
	}
	if s3.BackgroundFactsPerBaseRelation.Min <= batch.BackgroundFactsPerBaseRelation.Min {
		t.Error("scale3x background facts not scaled up")
	}
	if s10.BackgroundFactsPerBaseRelation.Min <= s3.BackgroundFactsPerBaseRelation.Min {
		t.Error("scale10x background facts not larger than scale3x")
	}
	if _, err := PresetConfig("nope", 7); err == nil {
		t.Error("unknown preset must error")
	}
}

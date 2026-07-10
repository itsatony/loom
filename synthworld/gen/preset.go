package gen

import "fmt"

// PresetConfig returns the named generation preset. Presets are the single
// source of truth shared by cmd/synthgen and cmd/batch; changing an existing
// preset breaks byte-identity of previously generated datasets, so existing
// presets are frozen — add new ones instead.
//
//   - small:  the original default config (sample-dataset is small, seed 1234).
//   - medium: larger world, more queries.
//   - batch:  medium-scale world tuned for the E0 batch protocol gates —
//     enough rules that >= 20 revision flip queries and >= 20 retained
//     controls survive per seed, via the existing NumRevisionPairs /
//     NumRevisionQueries knobs (no new Config fields: the config is embedded
//     in manifest.json, so new fields would break byte-identity of old
//     datasets).
func PresetConfig(name string, seed int64) (Config, error) {
	cfg := DefaultConfig(seed)
	switch name {
	case "small":
		// defaults
	case "medium":
		cfg.EntitiesPerType = Range{Min: 15, Max: 30}
		cfg.NumBaseRelations = 10
		cfg.RelationsPerStratum = 3
		cfg.SeedChainsPerRule = Range{Min: 3, Max: 6}
		cfg.BackgroundFactsPerBaseRelation = Range{Min: 15, Max: 30}
		cfg.NumRevisionPairs = 10
		cfg.NumRepetitionQueries = 60
		cfg.NumCompositionQueries = 80
		cfg.NumFindQueries = 20
		cfg.NumRevisionQueries = 24
	case "batch":
		// Balance constraint: composition candidates EXCLUDE atoms derived by
		// revision-pair rules (they belong to the revision slice), so the
		// world needs enough rules that ~20 revision pairs still leave a
		// healthy non-revision rule population for composition queries.
		cfg.EntitiesPerType = Range{Min: 15, Max: 30}
		cfg.NumBaseRelations = 10
		cfg.RelationsPerStratum = 3
		cfg.RulesPerDerivedRelation = Range{Min: 2, Max: 3}
		cfg.SeedChainsPerRule = Range{Min: 3, Max: 6}
		cfg.BackgroundFactsPerBaseRelation = Range{Min: 15, Max: 30}
		cfg.NumRevisionPairs = 14
		cfg.NumRepetitionQueries = 60
		cfg.NumCompositionQueries = 80
		cfg.NumFindQueries = 20
		cfg.NumRevisionQueries = 48
	case "scale3x", "scale10x":
		// E6 corpus-scaling tiers: identical query structure to batch —
		// the variable under test is CORPUS size (episode-text volume),
		// which is driven by background facts and entity counts, not by
		// query knobs. Rule counts stay at batch levels so closure depth
		// and revision structure remain comparable; only the haystack
		// grows. Targets: scale3x ~100-150k text tokens, scale10x
		// ~350-500k (past qwen36's 65k context — the c1c-impossible line).
		cfg.NumBaseRelations = 10
		cfg.RelationsPerStratum = 3
		cfg.RulesPerDerivedRelation = Range{Min: 2, Max: 3}
		cfg.SeedChainsPerRule = Range{Min: 3, Max: 6}
		cfg.NumRevisionPairs = 14
		cfg.NumRepetitionQueries = 60
		cfg.NumCompositionQueries = 80
		cfg.NumFindQueries = 20
		cfg.NumRevisionQueries = 48
		// Calibrated empirically (seed 1): corpus tokens ≈ 24k fixed +
		// ~49 per background fact at batch entity densities. First guess
		// (bg×3) yielded only 1.6× — background facts are the main but
		// not sole text driver, so the knobs below overshoot the naive
		// multiple to land in the token target bands.
		if name == "scale3x" {
			cfg.EntitiesPerType = Range{Min: 30, Max: 60}
			cfg.BackgroundFactsPerBaseRelation = Range{Min: 120, Max: 200}
		} else {
			// 10× corpus WITHOUT 10× per-relation fact density: dense
			// tables blow the oracle's 200k-binding join guard (measured:
			// seed 3 at 450-650 facts/relation exploded on rul_019).
			// Instead spread the haystack across 30 base relations at
			// batch-like density — join behavior stays batch-like, corpus
			// grows to target. Relation names wrap the pool with _N
			// suffixes deterministically, so this is byte-identity-safe
			// for other presets.
			cfg.EntitiesPerType = Range{Min: 40, Max: 80}
			cfg.NumBaseRelations = 30
			cfg.BackgroundFactsPerBaseRelation = Range{Min: 180, Max: 280}
		}
	case "frames":
		// Frames-v1 preset (MASTERPLAN §9.6.4/§9.6.8): batch-level v0 world
		// (the frames seed protocol gates on ALL v0 gates too), plus the
		// frames layer configured via FramesPreset — deliberately NOT in
		// Config, so v0 manifests stay byte-identical.
		cfg.EntitiesPerType = Range{Min: 15, Max: 30}
		cfg.NumBaseRelations = 10
		cfg.RelationsPerStratum = 3
		cfg.RulesPerDerivedRelation = Range{Min: 2, Max: 3}
		cfg.SeedChainsPerRule = Range{Min: 3, Max: 6}
		cfg.BackgroundFactsPerBaseRelation = Range{Min: 15, Max: 30}
		cfg.NumRevisionPairs = 14
		cfg.NumRepetitionQueries = 60
		cfg.NumCompositionQueries = 80
		cfg.NumFindQueries = 20
		cfg.NumRevisionQueries = 48
	default:
		return cfg, fmt.Errorf("unknown preset %q", name)
	}
	return cfg, nil
}

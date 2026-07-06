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
	default:
		return cfg, fmt.Errorf("unknown preset %q", name)
	}
	return cfg, nil
}

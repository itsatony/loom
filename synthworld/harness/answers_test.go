package harness

import (
	"reflect"
	"testing"
)

func TestParseHoldsAnswer(t *testing.T) {
	cases := []struct {
		in      string
		want    bool
		wantErr bool
	}{
		{"true", true, false},
		{"True.", true, false},
		{"  FALSE", false, false},
		{"false — the rule was superseded", false, false},
		{"The answer is true", true, false},
		{"I believe the answer is false.", false, false},
		{"it could be true or false", false, true}, // both words: refuse to guess
		{"yes", false, true},
		{"", false, true},
	}
	for _, c := range cases {
		got, err := parseHoldsAnswer(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestParseFindAnswer(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{`["a","b"]`, []string{"a", "b"}, false},
		{`The matches are: ["customer_03"] as shown.`, []string{"customer_03"}, false},
		{`[]`, []string{}, false},
		{`no brackets here`, nil, true},
		{`[not json]`, nil, true},
	}
	for _, c := range cases {
		got, err := parseFindAnswer(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

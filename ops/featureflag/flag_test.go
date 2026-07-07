package featureflag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3" // test-only; production code must not import it

	"github.com/dmitrymomot/forge/ops/featureflag"
)

func unmarshalFlags(t *testing.T, src string) featureflag.Flags {
	t.Helper()
	var doc struct {
		Flags featureflag.Flags `yaml:"flags"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	return doc.Flags
}

func TestFlagsUnmarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("scalar shorthand", func(t *testing.T) {
		t.Parallel()
		fs := unmarshalFlags(t, `
flags:
  dark_mode: true
  max_items: 25
  banner: "Summer sale"
  ratio: 1.5
`)
		assert.Equal(t, featureflag.Flag{Value: "true", Enabled: true, Rollout: 100}, fs["dark_mode"])
		assert.Equal(t, featureflag.Flag{Value: "25", Enabled: true, Rollout: 100}, fs["max_items"])
		assert.Equal(t, featureflag.Flag{Value: "Summer sale", Enabled: true, Rollout: 100}, fs["banner"])
		assert.Equal(t, featureflag.Flag{Value: "1.5", Enabled: true, Rollout: 100}, fs["ratio"])
	})

	t.Run("object form with defaults", func(t *testing.T) {
		t.Parallel()
		fs := unmarshalFlags(t, `
flags:
  new_checkout:
    value: true
    rollout: 25
    allow: [role:staff, cus_9f2k]
    deny: [segment:self_excluded]
  plain:
    value: "x"
`)
		assert.Equal(t, featureflag.Flag{
			Value:   "true",
			Enabled: true,
			Rollout: 25,
			Allow:   []string{"role:staff", "cus_9f2k"},
			Deny:    []string{"segment:self_excluded"},
		}, fs["new_checkout"])
		// omitted enabled → true, omitted rollout → 100
		assert.Equal(t, featureflag.Flag{Value: "x", Enabled: true, Rollout: 100}, fs["plain"])
	})

	t.Run("explicit disable", func(t *testing.T) {
		t.Parallel()
		fs := unmarshalFlags(t, "flags:\n  off_flag: {value: true, enabled: false}\n")
		assert.False(t, fs["off_flag"].Enabled)
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		cases := map[string]struct {
			src  string
			want error
		}{
			"rollout too high":  {"flags:\n  f: {value: true, rollout: 101}\n", featureflag.ErrInvalidRollout},
			"rollout negative":  {"flags:\n  f: {value: true, rollout: -1}\n", featureflag.ErrInvalidRollout},
			"empty key":         {"flags:\n  \"\": true\n", featureflag.ErrEmptyKey},
			"null value":        {"flags:\n  f:\n", featureflag.ErrInvalidFlag},
			"unknown field":     {"flags:\n  f: {value: true, rollut: 5}\n", featureflag.ErrInvalidFlag},
			"sequence value":    {"flags:\n  f: [a, b]\n", featureflag.ErrInvalidFlag},
			"non-string tokens": {"flags:\n  f: {value: true, allow: [1, 2]}\n", featureflag.ErrInvalidFlag},
			"empty token":       {"flags:\n  f: {value: true, deny: [\"\"]}\n", featureflag.ErrInvalidFlag},
			"bad enabled type":  {"flags:\n  f: {value: true, enabled: yes please}\n", featureflag.ErrInvalidFlag},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var doc struct {
					Flags featureflag.Flags `yaml:"flags"`
				}
				err := yaml.Unmarshal([]byte(tc.src), &doc)
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.want)
			})
		}
	})
}

func TestFlagZeroValueDisabled(t *testing.T) {
	t.Parallel()
	assert.False(t, featureflag.Flag{}.Enabled, "zero value must be fail-safe disabled")
}

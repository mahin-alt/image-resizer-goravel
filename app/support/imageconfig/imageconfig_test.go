package imageconfig_test

import (
	"testing"

	"goravel/app/support/imageconfig"
	// Blank-imported for its init(), which calls bootstrap.Boot() and wires
	// up the facades (facades.Config() in particular) that imageconfig's
	// functions depend on. See tests/test_case.go. Using the external
	// "_test" package here (rather than "package imageconfig") is what
	// avoids an import cycle, since goravel/tests transitively imports this
	// package.
	_ "goravel/tests"
)

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func TestClampQuality(t *testing.T) {
	cases := []struct {
		name     string
		input    *int
		expected int
	}{
		{"nil uses default", nil, imageconfig.DefaultQuality()},
		{"within range kept", intPtr(70), 70},
		{"below min clamped", intPtr(1), imageconfig.QualityMin()},
		{"above max clamped", intPtr(1000), imageconfig.QualityMax()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := imageconfig.ClampQuality(c.input)
			if got != c.expected {
				t.Errorf("ClampQuality(%v) = %d, want %d", c.input, got, c.expected)
			}
		})
	}
}

func TestResolveUpscale(t *testing.T) {
	if got := imageconfig.ResolveUpscale(nil); got != imageconfig.AllowUpscaleDefault() {
		t.Errorf("ResolveUpscale(nil) = %v, want default %v", got, imageconfig.AllowUpscaleDefault())
	}
	if got := imageconfig.ResolveUpscale(boolPtr(true)); got != true {
		t.Errorf("ResolveUpscale(true) = %v, want true", got)
	}
	if got := imageconfig.ResolveUpscale(boolPtr(false)); got != false {
		t.Errorf("ResolveUpscale(false) = %v, want false", got)
	}
}

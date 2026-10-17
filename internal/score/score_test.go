package score

import (
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s = %.4f, want %.4f", name, got, want)
	}
}

func TestTrackingCompanies(t *testing.T) {
	// 40 × log2(1+n) ÷ log2(33), worked by hand
	cases := map[int]float64{
		0:   0,
		1:   7.9295,
		3:   15.859,
		7:   23.7885,
		15:  31.718,
		31:  39.6475,
		32:  40,
		33:  40,
		500: 40,
	}
	for n, want := range cases {
		near(t, "tracking", Compute(Input{TrackingCompanies: n}).Parts.Tracking, want)
	}
}

func TestCookies(t *testing.T) {
	// 15 × min(1, log2(1+c) ÷ 5)
	cases := map[int]float64{0: 0, 1: 3, 3: 6, 7: 9, 15: 12, 31: 15, 32: 15, 1000: 15}
	for c, want := range cases {
		near(t, "cookies", Compute(Input{IdentifierCookies: c}).Parts.Cookies, want)
	}
}

func TestFingerprintingCaps(t *testing.T) {
	cases := map[int]float64{0: 0, 1: 5, 2: 10, 3: 15, 4: 15}
	techniques := []string{"canvas", "webgl", "audio", "fonts"}
	for n, want := range cases {
		near(t, "fingerprinting", Compute(Input{Fingerprinting: techniques[:n]}).Parts.Fingerprinting, want)
	}
}

func TestFlagParts(t *testing.T) {
	r := Compute(Input{AdTech: true, SessionRecording: true, KeyCapture: true})
	near(t, "ad tech", r.Parts.AdTech, 10)
	near(t, "recording", r.Parts.Recording, 10)
	near(t, "key capture", r.Parts.KeyCapture, 10)
	if r.Total != 30 {
		t.Errorf("total = %d, want 30", r.Total)
	}
	if off := Compute(Input{}); off.Parts != (Parts{}) || off.Total != 0 {
		t.Errorf("empty input scored %+v", off)
	}
}

func TestMaximumIsOneHundred(t *testing.T) {
	r := Compute(Input{
		TrackingCompanies: 99, AdTech: true, IdentifierCookies: 99,
		Fingerprinting:   []string{"canvas", "webgl", "audio", "fonts"},
		SessionRecording: true, KeyCapture: true,
	})
	if r.Total != 100 || r.Label != "Extremely nosy" {
		t.Errorf("max = %d %q, want 100 Extremely nosy", r.Total, r.Label)
	}
}

func TestRoundsTheSumOnce(t *testing.T) {
	// pin the half values directly: the sum is rounded once, not the parts
	for _, c := range []struct {
		sum  float64
		want int
	}{{0.49, 0}, {0.5, 1}, {49.49, 49}, {49.5, 50}, {99.5, 100}, {10.4999, 10}} {
		if got := round(c.sum); got != c.want {
			t.Errorf("round(%v) = %d, want %d", c.sum, got, c.want)
		}
	}
	r := Compute(Input{TrackingCompanies: 1, IdentifierCookies: 1})
	if r.Total != 11 { // 7.9295 + 3 = 10.9295
		t.Errorf("total = %d, want 11", r.Total)
	}
}

func TestLabels(t *testing.T) {
	cases := map[int]string{
		0: "Not nosy", 9: "Not nosy",
		10: "A little nosy", 29: "A little nosy",
		30: "Nosy", 54: "Nosy",
		55: "Very nosy", 79: "Very nosy",
		80: "Extremely nosy", 100: "Extremely nosy",
	}
	for s, want := range cases {
		if got := Label(s); got != want {
			t.Errorf("Label(%d) = %q, want %q", s, got, want)
		}
	}
}

func TestComputeUsesLabelOfTotal(t *testing.T) {
	r := Compute(Input{TrackingCompanies: 32, AdTech: true, IdentifierCookies: 31})
	if r.Total != 65 || r.Label != "Very nosy" || r.Formula != "v1" {
		t.Errorf("got %+v", r)
	}
}

func TestFinding(t *testing.T) {
	cases := []struct {
		in   Input
		want string
	}{
		{Input{}, "example.com contacted no tracking companies before you clicked anything"},
		{Input{TrackingCompanies: 1}, "example.com contacted 1 tracking company before you clicked anything"},
		{Input{TrackingCompanies: 27}, "example.com contacted 27 tracking companies before you clicked anything"},
		{Input{TrackingCompanies: 27, Fingerprinting: []string{"canvas"}}, "example.com contacted 27 tracking companies before you clicked anything and called browser fingerprinting APIs"},
		{Input{TrackingCompanies: 27, Fingerprinting: []string{"canvas"}, SessionRecording: true}, "example.com contacted 27 tracking companies before you clicked anything and loaded a session recording script"},
		{Input{TrackingCompanies: 2, SessionRecording: true, KeyCapture: true, Fingerprinting: []string{"audio"}}, "example.com contacted 2 tracking companies before you clicked anything and sent what was typed into a form to a third party before it was submitted"},
	}
	for _, c := range cases {
		if got := Finding("example.com", c.in); got != c.want {
			t.Errorf("\n got %q\nwant %q", got, c.want)
		}
	}
}

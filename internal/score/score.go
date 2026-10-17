package score

import (
	"fmt"
	"math"
)

// Version is stored in every report. Changing any weight below means a
// new version, never an edit to this one.
const Version = "v1"

const (
	maxTracking    = 40.0
	adTechPoints   = 10.0
	maxCookies     = 15.0
	perTechnique   = 5.0
	maxFingerprint = 15.0
	replayPoints   = 10.0
	keyPoints      = 10.0
)

// Input is what a scan measured, reduced to the numbers the formula uses.
type Input struct {
	TrackingCompanies int
	AdTech            bool
	IdentifierCookies int
	Fingerprinting    []string // distinct techniques: canvas, webgl, audio, fonts
	SessionRecording  bool
	KeyCapture        bool
}

// Parts are the points each row of the formula gave, before rounding.
type Parts struct {
	Tracking       float64 `json:"tracking_companies"`
	AdTech         float64 `json:"ad_tech"`
	Cookies        float64 `json:"tracking_cookies"`
	Fingerprinting float64 `json:"fingerprinting"`
	Recording      float64 `json:"session_recording"`
	KeyCapture     float64 `json:"key_capture"`
}

type Result struct {
	Formula string `json:"formula"`
	Parts   Parts  `json:"parts"`
	Total   int    `json:"score"`
	Label   string `json:"label"`
}

func Compute(in Input) Result {
	var p Parts
	// 40 × log2(1 + n) ÷ log2(33): full marks at 32 companies
	p.Tracking = math.Min(maxTracking, maxTracking*math.Log2(1+float64(in.TrackingCompanies))/math.Log2(33))
	if in.AdTech {
		p.AdTech = adTechPoints
	}
	// 15 × min(1, log2(1 + c) ÷ 5): full marks at 31 cookies
	p.Cookies = maxCookies * math.Min(1, math.Log2(1+float64(in.IdentifierCookies))/5)
	p.Fingerprinting = math.Min(maxFingerprint, perTechnique*float64(len(in.Fingerprinting)))
	if in.SessionRecording {
		p.Recording = replayPoints
	}
	if in.KeyCapture {
		p.KeyCapture = keyPoints
	}
	total := round(p.Tracking + p.AdTech + p.Cookies + p.Fingerprinting + p.Recording + p.KeyCapture)
	return Result{Formula: Version, Parts: p, Total: total, Label: Label(total)}
}

// the parts are summed unrounded and the sum is rounded once
func round(sum float64) int { return int(math.Round(sum)) }

func Label(score int) string {
	switch {
	case score < 10:
		return "Not nosy"
	case score < 30:
		return "A little nosy"
	case score < 55:
		return "Nosy"
	case score < 80:
		return "Very nosy"
	}
	return "Extremely nosy"
}

// Finding is the one-line summary, built from the measurements and never
// written by hand.
func Finding(site string, in Input) string {
	var line string
	switch in.TrackingCompanies {
	case 0:
		line = fmt.Sprintf("%s contacted no tracking companies before you clicked anything", site)
	case 1:
		line = fmt.Sprintf("%s contacted 1 tracking company before you clicked anything", site)
	default:
		line = fmt.Sprintf("%s contacted %d tracking companies before you clicked anything", site, in.TrackingCompanies)
	}
	switch {
	case in.KeyCapture:
		line += " and sent what was typed into a form to a third party before it was submitted"
	case in.SessionRecording:
		line += " and loaded a session recording script"
	case len(in.Fingerprinting) > 0:
		line += " and called browser fingerprinting APIs"
	}
	return line
}

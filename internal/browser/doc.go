// Package browser visits one page in Chromium as a first-time visitor and
// returns a capture.Capture. Each visit gets a fresh temporary profile that is
// deleted afterwards, or, against a browser that stays up, a browser context
// of its own that is disposed of when the visit ends. All traffic goes through
// the guard proxy, and the browser may not resolve names itself.
//
// Frames and workers run in other processes, so a second DevTools connection
// pauses each new one, turns Network on for it, and only then lets it run.
// An injected script, reporting through a binding that the page cannot reach
// afterwards, records fingerprinting calls and cookie writes.
package browser

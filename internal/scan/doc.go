// Package scan runs one scan: it checks the address, starts the guard proxy,
// has the browser visit the page, runs the detectors over what was recorded
// and scores it. The result type holds everything a terminal summary, a JSON
// document or a report needs, and is built from the capture alone, so the
// same capture always gives the same result.
package scan

// Package detect reads a capture and says what the page did: which companies
// received requests and which of them track, which cookies identify a
// browser, which scripts fingerprint it or record the session, whether typed
// text left the page, and whether the visit met a bot check or an empty page
// instead of the site.
//
// The domain index, the CDN blocklist and the session-replay vendor list are
// the ones the previous engine used. Rules v1 replaces them.
package detect

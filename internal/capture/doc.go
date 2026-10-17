// Package capture holds what one visit to a page recorded: the requests, the
// cookies, the instrumented API calls, the WebSocket frames and the facts
// about the page itself. It is plain data. The browser fills it in and the
// detectors read it, so the same capture always gives the same findings.
package capture

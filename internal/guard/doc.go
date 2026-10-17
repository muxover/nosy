// Package guard keeps a scan away from the user's own network. Every byte the
// browser sends goes through Proxy, which resolves names itself, refuses any
// destination that is not a public unicast address or not on port 80 or 443,
// and counts what is downloaded against a size cap.
//
// The address is checked on the resolved list and again on the socket right
// before connect, so a DNS answer that changes between the two cannot reach
// a private host. Test-only routing and DNS stubs exist only in builds made
// with the nosytest tag.
package guard

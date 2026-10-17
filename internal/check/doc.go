// Package check looks at a site without a browser: where its name leads, what
// its home page answers and which cookies that first answer sets. It makes the
// requests a browser-less client would make for the home page and follows
// redirects, all through the guard's dialer, and reads nothing else.
package check

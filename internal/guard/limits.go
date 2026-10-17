package guard

import "time"

// What one visit to a site may cost.
const (
	// MaxRedirects is the longest chain of redirects followed for one page.
	MaxRedirects = 10
	// HardStop is the longest a visit may last.
	HardStop = 20 * time.Second
	// MaxBytes is the most one visit may download.
	MaxBytes int64 = 60 << 20
)

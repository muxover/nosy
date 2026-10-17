//go:build nosytest && !linux && !windows

package integration

// sampleBrowserCPU is not available off Linux; done delivers -1.
func sampleBrowserCPU() (stop func(), done <-chan float64) {
	out := make(chan float64, 1)
	out <- -1
	return func() {}, out
}

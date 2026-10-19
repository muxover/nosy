package browser

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/browser"

	"github.com/muxover/nosy/internal/capture"
)

// the permissions a page can ask for; every one is denied before the page runs
var permissions = []browser.PermissionDescriptor{
	{Name: "geolocation"}, {Name: "notifications"}, {Name: "midi"}, {Name: "midi", Sysex: true},
	{Name: "camera"}, {Name: "camera", PanTiltZoom: true}, {Name: "microphone"},
	{Name: "clipboard-read"}, {Name: "clipboard-write"}, {Name: "clipboard-write", AllowWithoutSanitization: true},
	{Name: "screen-wake-lock"}, {Name: "system-wake-lock"}, {Name: "idle-detection"}, {Name: "window-management"},
	{Name: "local-fonts"}, {Name: "storage-access"}, {Name: "top-level-storage-access"}, {Name: "display-capture"},
	{Name: "payment-handler"}, {Name: "accelerometer"}, {Name: "gyroscope"}, {Name: "magnetometer"},
	{Name: "ambient-light-sensor"}, {Name: "nfc"}, {Name: "speaker-selection"}, {Name: "keyboard-lock"},
	{Name: "pointer-lock"}, {Name: "fullscreen", AllowWithoutGesture: true}, {Name: "persistent-storage"},
	{Name: "push", UserVisibleOnly: true}, {Name: "background-sync"}, {Name: "background-fetch"},
	{Name: "periodic-background-sync"},
}

// lockDown refuses downloads and every permission request at the browser, and
// listens for the downloads the page tries. A command the browser does not
// know is logged: the profile and the launch flags deny the same things.
func (d *devtools) lockDown(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	d.hmu.Lock()
	d.handlers[""] = func(ev any) {
		d.v.mu.Lock()
		defer d.v.mu.Unlock()
		switch e := ev.(type) {
		case *browser.EventDownloadWillBegin:
			if len(d.v.c.Downloads) < maxPopups {
				d.v.c.Downloads = append(d.v.c.Downloads, capture.Download{URL: clip(e.URL, 300), Filename: e.SuggestedFilename, GUID: e.GUID})
			}
		case *browser.EventDownloadProgress:
			for i := range d.v.c.Downloads {
				if d.v.c.Downloads[i].GUID == e.GUID {
					d.v.c.Downloads[i].State = string(e.State)
				}
			}
		}
	}
	d.hmu.Unlock()
	if _, err := d.call(ctx, "", browser.CommandSetDownloadBehavior, browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorDeny).WithEventsEnabled(true)); err != nil {
		d.v.logf("refuse downloads: %v", err)
	}
	for i := range permissions {
		p := &permissions[i]
		cmd := browser.SetPermission(p, browser.PermissionSettingDenied)
		if _, err := d.call(ctx, "", browser.CommandSetPermission, cmd); err != nil {
			d.v.logf("deny %s: %v", p.Name, err)
		}
	}
}

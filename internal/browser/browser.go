package browser

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/storage"
	cdptarget "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/chrome"
	"github.com/muxover/nosy/internal/guard"
)

//go:embed instrument.js
var instrument string

// The visit, in seconds from the moment navigation starts. Every phase has
// a cap so the whole thing ends inside the hard stop.
const (
	HardStop    = guard.HardStop
	quietFor    = 2 * time.Second
	scrollMax   = 3 * time.Second
	settle      = 3 * time.Second
	typeMax     = 2 * time.Second
	typeSettle  = 1500 * time.Millisecond
	collectTime = 1500 * time.Millisecond
	graceTime   = 2500 * time.Millisecond

	// the wait for a quiet network is capped at 8.5 of the default 20 seconds
	quietShare = 17.0 / 40

	bindingName = "__nosy"
	maxRequests = 3000
	maxPostData = 64 << 10
	maxHTML     = 300 << 10
	maxFields   = 10
)

type Options struct {
	Chrome  string // path to the browser, empty to search PATH
	Proxy   *guard.Proxy
	Token   string // text to type into fields
	NetLog  string // write the browser's own network log here
	Remote  string // DevTools address of a browser that stays up; the visit gets a context of its own in it
	Verbose func(format string, args ...any)

	// Duration is the longest the visit may last; HardStop when zero.
	Duration time.Duration
	// Mobile visits as a phone: a narrow, touch-enabled screen and a mobile user agent.
	Mobile bool
	// GPC sends the Global Privacy Control signal, as a header and to scripts.
	GPC bool

	// Progress is told what the visit is doing, for a person watching.
	Progress func(step string)
}

type visit struct {
	o    Options
	t0   time.Time
	mu   sync.Mutex
	reqs []*capture.Request
	cur  map[string]int
	kind map[string]network.ResourceType
	c    *capture.Capture

	hardStop    time.Duration
	quietCap    time.Duration
	mainFrame   cdp.FrameID
	contextID   cdp.BrowserContextID // this visit's own browser context, empty when it has the browser to itself
	redirects   map[string]int       // redirects followed per main-frame request
	stop        context.CancelFunc
	parent      context.Context // the browser, still alive after the hard stop
	attachedIDs map[string]bool
	lastNet     atomic.Int64
	sawNet      atomic.Bool
	loadMS      atomic.Int64
}

// Visit loads target once as a first-time visitor and records everything.
// The browser profile is created fresh and deleted before returning.
func Visit(ctx context.Context, target *url.URL, o Options) (*capture.Capture, error) {
	var (
		cmd          *exec.Cmd
		address      = o.Remote
		chromeCtx    context.Context
		cancelChrome context.CancelFunc
		contextID    cdp.BrowserContextID
	)
	if o.Remote != "" {
		// a browser that stays up: this visit gets a browser context of its own,
		// with no cookies, cache, storage or service workers from any other visit,
		// and its own way out through our proxy. The context and everything in it
		// is disposed of when the visit ends, and by the browser itself if the
		// connection is lost first.
		allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, o.Remote)
		defer cancelAlloc()
		var closeContext func()
		var err error
		if chromeCtx, contextID, closeContext, err = ownContext(allocCtx, o); err != nil {
			return nil, fmt.Errorf("start browser: %w", err)
		}
		defer closeContext()
	} else {
		profile, err := os.MkdirTemp("", "nosy-profile-")
		if err != nil {
			return nil, err
		}
		defer removeProfile(profile)
		if err := writePreferences(profile); err != nil {
			return nil, err
		}
		port, err := freePort()
		if err != nil {
			return nil, err
		}
		address = fmt.Sprintf("http://127.0.0.1:%d", port)
		opts := allocatorOptions(profile, port, o)
		opts = append(opts, chromedp.ModifyCmdFunc(func(c *exec.Cmd) {
			confine(c)
			cmd = c
		}))
		allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
		defer cancelAlloc()
		chromeCtx, cancelChrome = chromedp.NewContext(allocCtx)
		defer func() {
			cancelChrome()
			killTree(cmd)
		}()
	}

	hardStop := o.Duration
	if hardStop <= 0 {
		hardStop = HardStop
	}
	v := &visit{
		o:           o,
		hardStop:    hardStop,
		quietCap:    time.Duration(float64(hardStop) * quietShare),
		contextID:   contextID,
		redirects:   map[string]int{},
		cur:         map[string]int{},
		kind:        map[string]network.ResourceType{},
		attachedIDs: map[string]bool{},
		c:           &capture.Capture{Token: o.Token},
	}
	v.c.Page.Meta = map[string][]string{}

	visitCtx, stop := context.WithCancel(chromeCtx)
	defer stop()
	v.parent = chromeCtx
	v.listen(visitCtx, stop)

	if err := v.setup(visitCtx); err != nil {
		return nil, fmt.Errorf("start browser: %w", err)
	}

	if d, err := v.watchChildren(visitCtx, address); err != nil {
		v.logf("worker traffic is not recorded: %v", err)
	} else {
		defer d.Close()
	}

	v.t0 = time.Now()
	v.c.Page.ScannedAt = v.t0.UTC()
	v.lastNet.Store(v.t0.UnixNano())
	watchdog := time.AfterFunc(v.hardStop, func() {
		v.mu.Lock()
		v.c.Page.TimeLimitHit = true
		v.mu.Unlock()
		stop()
	})
	defer watchdog.Stop()

	v.run(visitCtx, target)
	v.c.Page.Duration = time.Since(v.t0)
	watchdog.Stop()

	// the browser writes its network log when it closes itself, not when killed
	if v.o.NetLog != "" && v.o.Remote == "" {
		cctx, ccancel := context.WithTimeout(chromeCtx, 2*time.Second)
		_ = chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error { return browser.Close().Do(ctx) }))
		ccancel()
		terminate(cmd)
		waitExit(cmd, 4*time.Second)
		time.Sleep(2 * time.Second)
	}
	if o.Remote == "" {
		cancelChrome()
		killTree(cmd)
	}
	v.finish()
	return v.c, nil
}

// ownContext opens a browser context for one visit in a browser that stays up,
// with a window and a tab in it. A context needs a window of its own: without
// one the browser cannot open a tab in it. The first, empty tab only holds the
// connection and is closed with the context.
func ownContext(allocCtx context.Context, o Options) (tab context.Context, id cdp.BrowserContextID, closeAll func(), err error) {
	hold, cancelHold := chromedp.NewContext(allocCtx)
	var targetID cdptarget.ID
	err = chromedp.Run(hold, chromedp.ActionFunc(func(ctx context.Context) error {
		ex := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser)
		var err error
		id, err = cdptarget.CreateBrowserContext().
			WithDisposeOnDetach(true).
			WithProxyServer("http://" + o.Proxy.Addr()).
			WithProxyBypassList("<-loopback>").Do(ex)
		if err != nil {
			return err
		}
		targetID, err = cdptarget.CreateTarget("about:blank").WithBrowserContextID(id).WithNewWindow(true).Do(ex)
		return err
	}))
	if err != nil {
		cancelHold()
		return nil, "", nil, err
	}
	tab, cancelTab := chromedp.NewContext(hold, chromedp.WithTargetID(targetID))
	closeAll = func() {
		cancelTab()
		// everything the visit stored goes with the context; if the browser is
		// already gone there is nothing left to dispose of
		cctx, cancel := context.WithTimeout(hold, 3*time.Second)
		defer cancel()
		err := chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return cdptarget.DisposeBrowserContext(id).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		}))
		if err != nil {
			o.logf("close the browser context: %v", err)
		}
		cancelHold()
	}
	return tab, id, closeAll, nil
}

func allocatorOptions(profile string, port int, o Options) []chromedp.ExecAllocatorOption {
	w, h := desktop.width, desktop.height
	if o.Mobile {
		w, h = phone.width, phone.height
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.UserDataDir(profile),
		noSandboxOff,
		chromedp.Flag("remote-debugging-port", fmt.Sprint(port)),
		chromedp.WindowSize(w, h),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("proxy-server", "http://"+o.Proxy.Addr()),
		// loopback and link-local hosts skip the proxy unless told otherwise
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		// the browser may not resolve names itself; only the proxy does
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"),
		chromedp.Flag("disable-quic", true),
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-crash-reporter", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-domain-reliability", true),
		chromedp.Flag("disable-client-side-phishing-detection", true),
		chromedp.Flag("disable-site-isolation-trials", true),
		// TrackingProtection3pcd: Chromium 141 blocks third-party cookies with the
		// reason ThirdPartyPhaseout whatever the profile says, which hides every
		// tracker cookie from the scan
		chromedp.Flag("disable-features", "IsolateOrigins,site-per-process,Translate,OptimizationHints,MediaRouter,AutofillServerCommunication,TrackingProtection3pcd"),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("hide-scrollbars", true),
		// no GPU here: software WebGL, so scripts that probe WebGL see what a normal browser offers
		chromedp.Flag("enable-unsafe-swiftshader", true),
	}
	opts = append(opts, testFlags()...)
	if o.Chrome != "" {
		opts = append(opts, chromedp.ExecPath(o.Chrome))
	}
	if o.NetLog != "" {
		opts = append(opts, chromedp.Flag("log-net-log", o.NetLog), chromedp.Flag("net-log-capture-mode", "IncludeSensitive"))
	}
	return opts
}

// screen is what a visit pretends to be sitting in front of.
type screen struct {
	name          string
	width, height int
	scale         float64
	mobile        bool
}

var (
	desktop = screen{"desktop", 1366, 768, 1, false}
	phone   = screen{"mobile", 390, 844, 3, true}
)

func (s screen) viewport() string { return fmt.Sprintf("%dx%d", s.width, s.height) }

// userAgent is Chrome's reduced user agent for the screen, so every scan of
// one device looks the same whatever machine runs it.
func (s screen) userAgent(major string) string {
	if s.mobile {
		return "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + major + ".0.0.0 Mobile Safari/537.36"
	}
	return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + major + ".0.0.0 Safari/537.36"
}

func (s screen) platform() (name, version string) {
	if s.mobile {
		return "Android", "10.0.0"
	}
	return "Linux", "6.0"
}

// gpcScript makes navigator.globalPrivacyControl true, as the signal's own
// specification has it for a browser that sends the header.
const gpcScript = `Object.defineProperty(Navigator.prototype, 'globalPrivacyControl', {get: function () { return true; }, configurable: true, enumerable: true});`

func (v *visit) setup(ctx context.Context) error {
	scr := desktop
	if v.o.Mobile {
		scr = phone
	}
	v.c.Page.Device, v.c.Page.Viewport, v.c.Page.GPC = scr.name, scr.viewport(), v.o.GPC
	actions := []chromedp.Action{
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, product, _, _, _, err := browser.GetVersion().Do(ctx)
			if err != nil {
				return err
			}
			if err := chrome.CheckVersion(product); err != nil {
				return err
			}
			v.c.Page.Chromium = product
			major, _, _ := strings.Cut(strings.TrimPrefix(product, "HeadlessChrome/"), ".")
			major, _, _ = strings.Cut(strings.TrimPrefix(major, "Chrome/"), ".")
			ua := scr.userAgent(major)
			v.c.Page.UserAgent = ua
			brands := []*emulation.UserAgentBrandVersion{
				{Brand: "Chromium", Version: major},
				{Brand: "Not?A_Brand", Version: "24"},
			}
			platform, platformVersion := scr.platform()
			meta := &emulation.UserAgentMetadata{
				Brands: brands, FullVersionList: brands,
				Platform: platform, PlatformVersion: platformVersion, Architecture: "x86", Bitness: "64",
				Mobile: scr.mobile,
			}
			if scr.mobile {
				meta.Architecture, meta.Bitness, meta.Model = "", "", "K"
			}
			return emulation.SetUserAgentOverride(ua).
				WithAcceptLanguage("en-US,en;q=0.9").
				WithPlatform(platform).
				WithUserAgentMetadata(meta).Do(ctx)
		}),
		emulation.SetDeviceMetricsOverride(int64(scr.width), int64(scr.height), scr.scale, scr.mobile),
	}
	if scr.mobile {
		actions = append(actions, emulation.SetTouchEmulationEnabled(true).WithMaxTouchPoints(5))
	}
	actions = append(actions,
		emulation.SetLocaleOverride().WithLocale("en-US"),
		// a visitor's tab is the one in front: the window is visible, and this makes
		// it the focused one too, so the page sees focus the way a visitor's does
		emulation.SetFocusEmulationEnabled(true),
		network.Enable(),
		network.ClearBrowserCookies(),
		network.ClearBrowserCache(),
		page.Enable(),
		runtime.Enable(),
		runtime.AddBinding(bindingName),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(instrument).Do(ctx)
			return err
		}),
	)
	if v.o.GPC {
		actions = append(actions,
			network.SetExtraHTTPHeaders(network.Headers{"Sec-GPC": "1"}),
			chromedp.ActionFunc(func(ctx context.Context) error {
				_, err := page.AddScriptToEvaluateOnNewDocument(gpcScript).Do(ctx)
				return err
			}),
		)
	}
	return chromedp.Run(ctx, actions...)
}

func (v *visit) step(s string) {
	if v.o.Progress != nil {
		v.o.Progress(s)
	}
}

func (v *visit) logf(format string, args ...any) { v.o.logf(format, args...) }

func (o Options) logf(format string, args ...any) {
	if o.Verbose != nil {
		o.Verbose(format, args...)
	}
}

func (v *visit) run(ctx context.Context, target *url.URL) {
	if c := chromedp.FromContext(ctx); c != nil && c.Target != nil {
		v.mu.Lock()
		v.mainFrame = cdp.FrameID(c.Target.TargetID)
		v.mu.Unlock()
	}
	v.step("Opening the page")
	if !v.navigate(ctx, target) {
		return
	}
	v.step("Waiting for the page to settle")
	v.waitQuiet(ctx)
	v.step("Scrolling like a reader")
	v.scroll(ctx)
	sleep(ctx, settle)
	v.step("Typing into the fields, without sending")
	v.typeIntoFields(ctx)
	sleep(ctx, typeSettle)
	v.step("Collecting what the page did")
	// after the hard stop or the size cap the visit is over but the browser is
	// still there: take what the page has, within a short fixed grace
	if ctx.Err() != nil {
		v.collect(v.parent, graceTime)
		return
	}
	v.collect(ctx, collectTime)
}

// navigate starts the page load. It returns false when the load could not
// even begin.
func (v *visit) navigate(ctx context.Context, target *url.URL) bool {
	var navErr string
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		frame, _, errText, _, err := page.Navigate(target.String()).Do(ctx)
		if err != nil {
			return err
		}
		v.mu.Lock()
		v.mainFrame = frame
		v.mu.Unlock()
		navErr = errText
		return nil
	}))
	if err == nil && navErr == "" {
		return true
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case v.c.Page.MainError != "":
		// the visit already ended for a reason of its own (too many redirects)
	case navErr != "":
		v.c.Page.MainError = navErr
	case !v.c.Page.TimeLimitHit:
		v.c.Page.MainError = err.Error()
	}
	return false
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// waitQuiet returns once no request has started or received data for two
// seconds, or at the cap.
func (v *visit) waitQuiet(ctx context.Context) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if time.Since(v.t0) >= v.quietCap {
			v.mu.Lock()
			v.c.Page.NeverQuiet = true
			v.mu.Unlock()
			return
		}
		last := time.Unix(0, v.lastNet.Load())
		if v.sawNet.Load() && time.Since(last) >= quietFor {
			return
		}
	}
}

const scrollJS = `new Promise(function (done) {
  var start = Date.now(), dur = %d;
  (function step() {
    var el = document.scrollingElement || document.documentElement;
    var max = Math.max(0, el.scrollHeight - window.innerHeight);
    var t = Math.min(1, (Date.now() - start) / dur);
    window.scrollTo(0, max * t);
    if (t >= 1) return done(true);
    setTimeout(step, 50);
  })();
})`

func (v *visit) scroll(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, scrollMax+300*time.Millisecond)
	defer cancel()
	_ = chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, exp, err := runtime.Evaluate(fmt.Sprintf(scrollJS, scrollMax.Milliseconds())).WithAwaitPromise(true).Do(ctx)
		if err != nil {
			return err
		}
		if exp != nil {
			return exp
		}
		return nil
	}))
}

// visible text fields, in document order
const fieldsJS = `(function () {
  var out = [];
  var sel = 'input:not([type]), input[type=text], input[type=search], input[type=email], input[type=tel], input[type=url], textarea';
  var els = document.querySelectorAll(sel);
  for (var i = 0; i < els.length && out.length < %d; i++) {
    var e = els[i], r = e.getBoundingClientRect(), s = getComputedStyle(e);
    if (e.disabled || e.readOnly || r.width < 4 || r.height < 4) continue;
    if (s.visibility === 'hidden' || s.display === 'none' || parseFloat(s.opacity) === 0) continue;
    e.setAttribute('data-nosy-field', String(out.length));
    out.push(out.length);
  }
  return out.length;
})()`

// typeIntoFields focuses each visible text field and types the test string
// one key at a time. It never clicks and never submits.
func (v *visit) typeIntoFields(ctx context.Context) {
	if v.o.Token == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, typeMax)
	defer cancel()
	var n int
	if err := chromedp.Run(cctx, chromedp.Evaluate(fmt.Sprintf(fieldsJS, maxFields), &n)); err != nil {
		v.logf("find fields: %v", err)
		return
	}
	typed := 0
	for i := 0; i < n && cctx.Err() == nil; i++ {
		err := chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
			if _, exp, err := runtime.Evaluate(fmt.Sprintf(`document.querySelector('[data-nosy-field="%d"]').focus()`, i)).Do(ctx); err != nil || exp != nil {
				return errors.New("focus failed")
			}
			for _, ch := range v.o.Token {
				s := string(ch)
				if err := input.DispatchKeyEvent(input.KeyDown).WithText(s).WithKey(s).Do(ctx); err != nil {
					return err
				}
				if err := input.DispatchKeyEvent(input.KeyUp).WithKey(s).Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}))
		if err == nil {
			typed++
		}
	}
	v.mu.Lock()
	v.c.Page.TypedInto = typed
	v.mu.Unlock()
}

// the page facts every check reads, gathered in one round trip
const pageJS = `(function () {
  var out = {};
  var body = document.body;
  var text = body ? body.innerText || '' : '';
  out.title = document.title || '';
  out.text = text.slice(0, 6000);
  out.textLen = text.trim().length;
  out.elements = document.getElementsByTagName('*').length;
  out.html = document.documentElement ? document.documentElement.outerHTML.slice(0, %d) : '';
  out.scripts = Array.prototype.map.call(document.scripts, function (s) { return s.src; }).filter(Boolean);
  out.meta = {};
  Array.prototype.forEach.call(document.getElementsByTagName('meta'), function (m) {
    var k = (m.getAttribute('name') || m.getAttribute('property') || '').toLowerCase();
    if (k) (out.meta[k] = out.meta[k] || []).push(m.getAttribute('content') || '');
  });
  out.consent = [];
  ['__tcfapi', '__cmp', '__uspapi', '__gpp'].forEach(function (n) { if (typeof window[n] === 'function') out.consent.push(n); });
  return out;
})`

type pageData struct {
	Title    string              `json:"title"`
	Text     string              `json:"text"`
	TextLen  int                 `json:"textLen"`
	Elements int                 `json:"elements"`
	HTML     string              `json:"html"`
	Scripts  []string            `json:"scripts"`
	Meta     map[string][]string `json:"meta"`
	Consent  []string            `json:"consent"`
}

func (v *visit) collect(ctx context.Context, budget time.Duration) {
	cctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	var cookies []*network.Cookie
	if err := chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		if v.contextID != "" {
			// the cookie jar of this visit's own context, not the browser's default one;
			// the browser answers this one, not the page
			ex := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser)
			cookies, err = storage.GetCookies().WithBrowserContextID(v.contextID).Do(ex)
			return err
		}
		cookies, err = storage.GetCookies().Do(ctx)
		return err
	})); err != nil {
		v.logf("cookies: %v", err)
	}

	expr := fmt.Sprintf(pageJS, maxHTML) + "()"
	var pd pageData
	if err := chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
		res, exp, err := runtime.Evaluate(expr).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if exp != nil {
			return exp
		}
		return json.Unmarshal(res.Value, &pd)
	})); err != nil {
		v.logf("page data: %v", err)
	}

	var finalURL string
	_ = chromedp.Run(cctx, chromedp.Location(&finalURL))

	// a visit that hit its time or size limit has no time to read storage
	v.mu.Lock()
	over := v.c.Page.TimeLimitHit || v.c.Page.SizeCapHit || v.c.Page.Crashed
	v.mu.Unlock()
	if !over {
		v.collectStorage(ctx)
	}

	v.fetchPostData(cctx)

	v.mu.Lock()
	defer v.mu.Unlock()
	pg := &v.c.Page
	pg.Title, pg.Text, pg.TextLen, pg.Elements = pd.Title, pd.Text, pd.TextLen, pd.Elements
	pg.HTML, pg.Scripts, pg.Meta, pg.ConsentAPIs = pd.HTML, pd.Scripts, pd.Meta, pd.Consent
	if finalURL != "" {
		pg.FinalURL = finalURL
	}
	for _, c := range cookies {
		v.c.Cookies = append(v.c.Cookies, capture.Cookie{
			Name: c.Name, Domain: c.Domain, Path: c.Path, Expires: c.Expires, Session: c.Session,
			Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: string(c.SameSite), Size: int(c.Size), Value: c.Value,
		})
	}
}

// bodies the event did not carry are fetched for the few requests that
// could contain typed text
func (v *visit) fetchPostData(ctx context.Context) {
	v.mu.Lock()
	var ids []string
	for id, i := range v.cur {
		r := v.reqs[i]
		// only the page's own requests: other targets have their own sessions
		if r.Method != "GET" && r.PostData == "" && len(ids) < 50 && !strings.Contains(id, "|") {
			ids = append(ids, id)
		}
	}
	v.mu.Unlock()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		var body string
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			b, err := network.GetRequestPostData(network.RequestID(id)).Do(ctx)
			body = string(b)
			return err
		}))
		if err != nil || body == "" {
			continue
		}
		v.mu.Lock()
		if i, ok := v.cur[id]; ok {
			v.reqs[i].PostData = clip(body, maxPostData)
		}
		v.mu.Unlock()
	}
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (v *visit) finish() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, r := range v.reqs {
		v.c.Requests = append(v.c.Requests, *r)
	}
	v.c.Requests = capture.WithoutBrowserOwn(v.c.Requests)
	v.c.Page.LoadMS = v.loadMS.Load()
	v.c.Proxy = v.o.Proxy.Stats()
	for i := range v.c.Requests {
		r := &v.c.Requests[i]
		if ips := v.c.Proxy.HostIPs[r.Host]; len(ips) > 0 {
			r.IP = ips[0]
		}
		markRefused(r, v.c.Proxy)
		if r.MainFrame {
			v.c.Page.MainStatus = r.Status
			if r.Refused {
				v.c.Page.MainError = "refused by the safety guard"
			}
			if r.Failed != "" && v.c.Page.MainError == "" {
				v.c.Page.MainError = r.Failed
			}
		}
	}
	if v.c.Page.FinalURL == "" {
		for i := len(v.c.Requests) - 1; i >= 0; i-- {
			if v.c.Requests[i].MainFrame {
				v.c.Page.FinalURL = v.c.Requests[i].URL
				break
			}
		}
	}
	v.c.Page.SizeCapHit = v.c.Proxy.CapHit
}

// markRefused flags a request to a destination the safety guard turned away.
func markRefused(r *capture.Request, st guard.Stats) {
	for _, rf := range st.Refused {
		if rf.Host == r.Host && (rf.Port == r.Port || r.Port == "") {
			r.Refused = true
		}
	}
}

func decodeEntries(es []*network.PostDataEntry) string {
	var sb strings.Builder
	for _, e := range es {
		b, err := base64.StdEncoding.DecodeString(e.Bytes)
		if err != nil {
			continue
		}
		sb.Write(b)
		if sb.Len() >= maxPostData {
			break
		}
	}
	return clip(sb.String(), maxPostData)
}

// writePreferences starts the fresh profile the way a default desktop
// Chrome behaves: third-party cookies allowed. Some Chromium builds block
// them out of the box, which would hide every tracker cookie from the scan.
func writePreferences(profile string) error {
	dir := filepath.Join(profile, "Default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	prefs := `{"profile":{"cookie_controls_mode":0,"default_content_setting_values":{"cookies":1}}}`
	return os.WriteFile(filepath.Join(dir, "Preferences"), []byte(prefs), 0o600)
}

// removeProfile deletes a profile directory. The browser's helper processes may
// still be closing files in it just after the browser is killed, so a failed
// removal is retried for a couple of seconds.
func removeProfile(dir string) {
	for i := 0; i < 20; i++ {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// noSandboxOff keeps the sandbox on. chromedp adds --no-sandbox by itself when
// the process runs as root, unless the flag is set, to false, first.
var noSandboxOff = chromedp.Flag("no-sandbox", false)

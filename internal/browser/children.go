package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/page"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// Workers and frames the browser runs in other processes are not part of the
// page's DevTools session, and chromedp offers no way to drive their sessions
// in the order that matters: Network must be on before a worker runs its
// first line. So a second, small DevTools connection asks the browser to
// pause every new worker, turns Network on for it, and only then lets it go.

type devtools struct {
	v    *visit
	conn net.Conn
	mu   sync.Mutex
	next int64
	once sync.Once

	hmu      sync.Mutex
	handlers map[string]func(ev any) // by session id
	pending  map[int64]chan reply
	frames   map[string]string // session id by target id, for the frames in other processes
	ready    chan struct{}
}

type reply struct {
	result json.RawMessage
	err    error
}

// childCmd is a command every frame in another process gets before its first
// line runs, so that it is watched the way the page is.
type childCmd struct {
	method string
	params any
}

// the target types whose traffic belongs to the visit
var watched = map[string]bool{"worker": true, "shared_worker": true, "service_worker": true}

func (v *visit) watchChildren(ctx context.Context, address string) (*devtools, error) {
	wsURL, err := browserWebSocket(ctx, address)
	if err != nil {
		return nil, err
	}
	conn, _, _, err := ws.Dial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	d := &devtools{v: v, conn: conn, handlers: map[string]func(any){}, pending: map[int64]chan reply{}, frames: map[string]string{}, ready: make(chan struct{})}
	go d.read()
	d.lockDown(ctx)
	// the page and the browser-wide workers; the page's own workers and frames
	// are attached from its session below
	d.send("", "Target.setAutoAttach", map[string]any{
		"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
		"filter": []map[string]any{{"type": "page"}, {"type": "shared_worker"}, {"type": "service_worker"}, {"exclude": true}},
	})
	select {
	case <-d.ready:
	case <-time.After(time.Second):
	}
	return d, nil
}

func (d *devtools) Close() { d.conn.Close() }

// browserWebSocket asks the browser for its DevTools address. A browser
// behind a forwarder reports its own loopback address, so only the path of
// that answer is used.
func browserWebSocket(ctx context.Context, address string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/json/version", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var info struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	u, err := url.Parse(info.URL)
	if err != nil {
		return "", err
	}
	base, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	return "ws://" + base.Host + u.Path, nil
}

func (d *devtools) send(session, method string, params any) {
	d.mu.Lock()
	d.next++
	msg := map[string]any{"id": d.next, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	_ = wsutil.WriteClientText(d.conn, b)
	d.mu.Unlock()
}

// call sends a command and waits for the answer.
func (d *devtools) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	ch := make(chan reply, 1)
	d.mu.Lock()
	d.next++
	id := d.next
	d.pending[id] = ch
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	err := wsutil.WriteClientText(d.conn, b)
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		d.mu.Lock()
		delete(d.pending, id)
		d.mu.Unlock()
		return nil, ctx.Err()
	}
}

// frameSessions lists the sessions of the frames that run in other processes.
func (d *devtools) frameSessions() []string {
	d.hmu.Lock()
	defer d.hmu.Unlock()
	out := make([]string, 0, len(d.frames))
	for _, s := range d.frames {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (d *devtools) sessionOf(target string) (string, bool) {
	d.hmu.Lock()
	defer d.hmu.Unlock()
	s, ok := d.frames[target]
	return s, ok
}

type devtoolsMessage struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"sessionId"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (d *devtools) read() {
	for {
		data, err := wsutil.ReadServerText(d.conn)
		if err != nil {
			return
		}
		var m devtoolsMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Method == "" {
			d.answer(m)
			continue
		}
		if m.Method == "Target.attachedToTarget" {
			d.attached(m.Params)
			continue
		}
		if m.Method == "Target.detachedFromTarget" {
			d.detached(m.Params)
			continue
		}
		d.hmu.Lock()
		h := d.handlers[m.SessionID]
		d.hmu.Unlock()
		if h == nil {
			continue
		}
		if ev, err := cdproto.UnmarshalMessage(&cdproto.Message{Method: cdproto.MethodType(m.Method), Params: jsontext.Value(m.Params)}); err == nil {
			h(ev)
		}
	}
}

func (d *devtools) answer(m devtoolsMessage) {
	d.mu.Lock()
	ch := d.pending[m.ID]
	delete(d.pending, m.ID)
	d.mu.Unlock()
	if ch == nil {
		return
	}
	r := reply{result: m.Result}
	if m.Error != nil {
		r.err = errors.New(m.Error.Message)
	}
	ch <- r
}

func (d *devtools) detached(raw json.RawMessage) {
	var e struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	d.hmu.Lock()
	for t, s := range d.frames {
		if s == e.SessionID {
			delete(d.frames, t)
		}
	}
	d.hmu.Unlock()
}

func (d *devtools) attached(raw json.RawMessage) {
	var e struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	d.v.logf("attached %s %s waiting=%v", e.TargetInfo.Type, e.TargetInfo.URL, e.WaitingForDebugger)
	resume := func() {
		if e.WaitingForDebugger {
			d.send(e.SessionID, "Runtime.runIfWaitingForDebugger", map[string]any{})
		}
	}
	typ := e.TargetInfo.Type
	d.v.mu.Lock()
	seen := d.v.attachedIDs[e.TargetInfo.TargetID]
	d.v.attachedIDs[e.TargetInfo.TargetID] = true
	d.v.mu.Unlock()
	switch {
	case typ == "page" && d.v.isPopup(e.TargetInfo.TargetID):
		// a window the page opened: nothing of it is loaded, and it is closed
		d.v.mu.Lock()
		d.v.c.Page.PopupsClosed++
		d.v.mu.Unlock()
		d.v.onNavigationRequest(e.TargetInfo.URL)
		d.send("", "Target.closeTarget", map[string]any{"targetId": e.TargetInfo.TargetID})
	case typ == "page" && strings.HasPrefix(e.TargetInfo.URL, "chrome://"):
		// the browser's own new tab page: its frames are not the site's, so they are not recorded
		resume()
	case typ == "page" && !d.v.isPopup(e.TargetInfo.TargetID):
		d.once.Do(func() { close(d.ready) })
		// its workers and frames start paused for us
		d.send(e.SessionID, "Target.setAutoAttach", map[string]any{
			"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
			"filter": []map[string]any{{"type": "worker"}, {"type": "shared_worker"}, {"type": "iframe"}, {"exclude": true}},
		})
		resume()
	case typ == "iframe" && !seen:
		h := d.v.handler(d.v.parent, e.TargetInfo.TargetID+"|")
		d.hmu.Lock()
		d.handlers[e.SessionID] = func(ev any) {
			if _, ok := ev.(*page.EventJavascriptDialogOpening); ok {
				d.send(e.SessionID, "Page.handleJavaScriptDialog", map[string]any{"accept": false})
				return
			}
			h(ev)
		}
		d.frames[e.TargetInfo.TargetID] = e.SessionID
		d.hmu.Unlock()
		for _, c := range d.v.childCmds {
			d.send(e.SessionID, c.method, c.params)
		}
		// a frame can hold frames and start workers
		d.send(e.SessionID, "Target.setAutoAttach", map[string]any{
			"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
			"filter": []map[string]any{{"type": "worker"}, {"type": "iframe"}, {"exclude": true}},
		})
		resume()
	case watched[typ] && !seen:
		d.hmu.Lock()
		d.handlers[e.SessionID] = d.v.handler(d.v.parent, e.TargetInfo.TargetID+"|")
		d.hmu.Unlock()
		d.send(e.SessionID, "Network.enable", map[string]any{})
		// a worker can start more workers
		d.send(e.SessionID, "Target.setAutoAttach", map[string]any{
			"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
			"filter": []map[string]any{{"type": "worker"}, {"exclude": true}},
		})
		resume()
	default:
		resume()
	}
}

// freePort picks a local port for the browser's DevTools.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

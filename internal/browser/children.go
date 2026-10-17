package browser

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
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
	ready    chan struct{}
}

// the target types whose traffic belongs to the visit
var watched = map[string]bool{"worker": true, "shared_worker": true, "service_worker": true, "iframe": true}

func (v *visit) watchChildren(ctx context.Context, address string) (*devtools, error) {
	wsURL, err := browserWebSocket(ctx, address)
	if err != nil {
		return nil, err
	}
	conn, _, _, err := ws.Dial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	d := &devtools{v: v, conn: conn, handlers: map[string]func(any){}, ready: make(chan struct{})}
	go d.read()
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

type devtoolsMessage struct {
	SessionID string          `json:"sessionId"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
}

func (d *devtools) read() {
	for {
		data, err := wsutil.ReadServerText(d.conn)
		if err != nil {
			return
		}
		var m devtoolsMessage
		if json.Unmarshal(data, &m) != nil || m.Method == "" {
			continue
		}
		if m.Method == "Target.attachedToTarget" {
			d.attached(m.Params)
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

func (d *devtools) attached(raw json.RawMessage) {
	var e struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			TargetID  string `json:"targetId"`
			Type      string `json:"type"`
			URL       string `json:"url"`
			ContextID string `json:"browserContextId"`
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
	// a browser that runs several scans has other contexts: their pages and workers
	// belong to those scans, and are let go without being recorded
	if d.v.contextID != "" && e.TargetInfo.ContextID != string(d.v.contextID) {
		resume()
		return
	}
	d.v.mu.Lock()
	seen := d.v.attachedIDs[e.TargetInfo.TargetID]
	d.v.attachedIDs[e.TargetInfo.TargetID] = true
	d.v.mu.Unlock()
	switch {
	case typ == "page" && strings.HasPrefix(e.TargetInfo.URL, "chrome://"):
		// the browser's own new tab page: its frames are not the site's, so they are not recorded
		resume()
	case typ == "page":
		d.once.Do(func() { close(d.ready) })
		// its workers and frames start paused for us
		d.send(e.SessionID, "Target.setAutoAttach", map[string]any{
			"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true,
			"filter": []map[string]any{{"type": "worker"}, {"type": "shared_worker"}, {"type": "iframe"}, {"exclude": true}},
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

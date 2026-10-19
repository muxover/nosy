package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/muxover/nosy/internal/capture"
)

//go:embed storage.js
var storageJS string

const (
	maxStorageFrames = 20
	storageBudget    = 8 * time.Second
)

type storageItem struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Length int    `json:"length"`
}

type storageDump struct {
	Origin  string        `json:"origin"`
	Cut     bool          `json:"cut"`
	Local   []storageItem `json:"local"`
	Session []storageItem `json:"session"`
	IDB     []struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
		Stores  []struct {
			Name    string        `json:"name"`
			Entries int           `json:"entries"`
			Keys    []string      `json:"keys"`
			Values  []storageItem `json:"values"`
		} `json:"stores"`
	} `json:"idb"`
	Caches []string `json:"caches"`
	SW     []struct {
		Scope  string `json:"scope"`
		Script string `json:"script"`
	} `json:"sw"`
}

// collectStorage reads what each frame's origin has kept in the browser. A
// script in a world of its own in the frame sees that frame's storage, which
// for a third-party frame is the storage partitioned to this site.
func (v *visit) collectStorage(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, storageBudget)
	defer cancel()

	var frames []cdp.FrameID
	err := chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		var walk func(t *page.FrameTree)
		walk = func(t *page.FrameTree) {
			if len(frames) >= maxStorageFrames {
				return
			}
			if u, err := url.Parse(t.Frame.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
				frames = append(frames, t.Frame.ID)
			}
			for _, c := range t.ChildFrames {
				walk(c)
			}
		}
		walk(tree)
		return nil
	}))
	if err != nil {
		v.logf("storage: frames: %v", err)
		return
	}

	seen := map[string]bool{}
	var out []capture.Storage
	cut := false
	add := func(raw string) {
		var d storageDump
		if json.Unmarshal([]byte(raw), &d) != nil {
			return
		}
		cut = cut || d.Cut
		if d.Origin == "" || d.Origin == "null" || seen[d.Origin] {
			return
		}
		seen[d.Origin] = true
		if s := toStorage(d); !s.Empty() {
			out = append(out, s)
		}
	}
	for _, id := range frames {
		if cctx.Err() != nil {
			cut = true
			break
		}
		if v.dt != nil {
			// a frame in another process is read over its own session below
			if _, other := v.dt.sessionOf(string(id)); other {
				continue
			}
		}
		var raw string
		err := chromedp.Run(cctx, chromedp.ActionFunc(func(ctx context.Context) error {
			world, err := page.CreateIsolatedWorld(id).WithWorldName("nosy-storage").Do(ctx)
			if err != nil {
				return err
			}
			res, exp, err := runtime.Evaluate(storageJS).WithContextID(world).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
			if err != nil {
				return err
			}
			if exp != nil {
				return exp
			}
			return json.Unmarshal(res.Value, &raw)
		}))
		if err != nil {
			v.logf("storage: frame %s: %v", id, err)
			cut = cut || cctx.Err() != nil
			continue
		}
		add(raw)
	}
	if v.dt != nil {
		for _, session := range v.dt.frameSessions() {
			if cctx.Err() != nil {
				cut = true
				break
			}
			if err := v.frameStorage(cctx, session, add); err != nil {
				v.logf("storage: frame in another process: %v", err)
				cut = cut || cctx.Err() != nil
			}
		}
	}
	v.mu.Lock()
	v.c.Storage = out
	v.c.Page.StorageCutShort = v.c.Page.StorageCutShort || cut
	v.mu.Unlock()
}

// frameStorage reads the frames of one frame process, over its own session:
// the page's session cannot reach into another process.
func (v *visit) frameStorage(ctx context.Context, session string, add func(raw string)) error {
	res, err := v.dt.call(ctx, session, "Page.getFrameTree", struct{}{})
	if err != nil {
		return err
	}
	var tree struct {
		FrameTree frameNode `json:"frameTree"`
	}
	if err := json.Unmarshal(res, &tree); err != nil {
		return err
	}
	var ids []string
	var walk func(n frameNode)
	walk = func(n frameNode) {
		if len(ids) >= maxStorageFrames {
			return
		}
		if u, err := url.Parse(n.Frame.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			ids = append(ids, n.Frame.ID)
		}
		for _, c := range n.ChildFrames {
			walk(c)
		}
	}
	walk(tree.FrameTree)
	for _, id := range ids {
		res, err := v.dt.call(ctx, session, "Page.createIsolatedWorld", map[string]any{"frameId": id, "worldName": "nosy-storage"})
		if err != nil {
			return err
		}
		var w struct {
			ContextID int64 `json:"executionContextId"`
		}
		if err := json.Unmarshal(res, &w); err != nil {
			return err
		}
		res, err = v.dt.call(ctx, session, "Runtime.evaluate", map[string]any{
			"expression": storageJS, "contextId": w.ContextID, "awaitPromise": true, "returnByValue": true,
		})
		if err != nil {
			return err
		}
		var ev struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
			Exception *struct {
				Text string `json:"text"`
			} `json:"exceptionDetails"`
		}
		if err := json.Unmarshal(res, &ev); err != nil {
			return err
		}
		if ev.Exception != nil {
			return errors.New(ev.Exception.Text)
		}
		add(ev.Result.Value)
	}
	return nil
}

type frameNode struct {
	Frame struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	} `json:"frame"`
	ChildFrames []frameNode `json:"childFrames"`
}

func toStorage(d storageDump) capture.Storage {
	s := capture.Storage{Origin: d.Origin, Caches: d.Caches}
	for _, it := range d.Local {
		s.Local = append(s.Local, capture.StorageItem{Key: it.Key, Value: it.Value, Length: it.Length})
	}
	for _, it := range d.Session {
		s.Session = append(s.Session, capture.StorageItem{Key: it.Key, Value: it.Value, Length: it.Length})
	}
	for _, db := range d.IDB {
		out := capture.IDBDatabase{Name: db.Name, Version: db.Version}
		for _, st := range db.Stores {
			store := capture.IDBStore{Name: st.Name, Entries: st.Entries, Keys: st.Keys}
			for _, val := range st.Values {
				store.Values = append(store.Values, capture.StorageItem{Key: val.Key, Value: val.Value, Length: val.Length})
			}
			out.Stores = append(out.Stores, store)
		}
		s.IndexedDB = append(s.IndexedDB, out)
	}
	for _, w := range d.SW {
		s.ServiceWorkers = append(s.ServiceWorkers, capture.ServiceWorker{Scope: w.Scope, Script: w.Script})
	}
	return s
}

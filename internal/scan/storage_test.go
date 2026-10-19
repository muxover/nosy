package scan

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/chrome"
	"github.com/muxover/nosy/internal/detect"
)

func sampleStorage() []capture.Storage {
	return []capture.Storage{
		{
			Origin: "https://www.shop.test",
			Local: []capture.StorageItem{
				{Key: "uid", Value: "9f86d081884c7d659a2feaa0c55ad015", Length: 32},
				{Key: "theme", Value: "dark", Length: 4},
				{Key: "seen", Value: "2026-10-01", Length: 10},
			},
			Session: []capture.StorageItem{{Key: "tab", Value: "a1b2c3d4e5f6a7b8c9d0e1f2", Length: 24}},
			IndexedDB: []capture.IDBDatabase{{Name: "app", Version: 2, Stores: []capture.IDBStore{
				{Name: "users", Entries: 2, Keys: []string{"a", "b"}, Values: []capture.StorageItem{
					{Key: "visitor", Value: "3f2b8a4e-9c1d-4e7a-b2f0-6d5a1c8e9b73"},
					{Key: "visitor", Value: "0a1b2c3d-9c1d-4e7a-b2f0-6d5a1c8e9b73"},
					{Key: "name", Value: "Ada"},
				}},
			}}},
			Caches:         []string{"v1"},
			ServiceWorkers: []capture.ServiceWorker{{Scope: "https://www.shop.test/", Script: "https://www.shop.test/sw.js"}},
		},
		{
			Origin: "https://ads.partner.test",
			Local:  []capture.StorageItem{{Key: "pid", Value: "5d41402abc4b2a76b9719d911017c592", Length: 32}},
		},
		{
			Origin: "https://assets.owned.test",
			Local:  []capture.StorageItem{{Key: "x", Value: "y", Length: 1}},
		},
	}
}

func TestStorageSeparatesFirstAndThirdParties(t *testing.T) {
	index := detect.NewIndex(nil, map[string][]string{"Shop Co": {"shop.test", "owned.test"}})
	got := storage(&capture.Capture{Storage: sampleStorage()}, "shop.test", index)
	if len(got) != 3 {
		t.Fatalf("%d origins", len(got))
	}
	if got[0].ThirdParty || !got[1].ThirdParty || got[2].ThirdParty {
		t.Errorf("third party flags = %v %v %v, want first, third, first (same owner)", got[0].ThirdParty, got[1].ThirdParty, got[2].ThirdParty)
	}
}

func TestStorageValuesAreCheckedWithTheCookieIdentifierRule(t *testing.T) {
	got := storage(&capture.Capture{Storage: sampleStorage()}, "shop.test", detect.NewIndex(nil, nil))
	first := got[0]
	want := map[string]bool{"uid": true, "theme": false, "seen": false}
	for _, k := range first.LocalStorage {
		if k.Identifier != want[k.Key] {
			t.Errorf("localStorage %s identifier = %v", k.Key, k.Identifier)
		}
	}
	// gone with the tab, so never an identifier whatever it looks like
	if len(first.SessionStorage) != 1 || first.SessionStorage[0].Identifier {
		t.Errorf("sessionStorage = %+v", first.SessionStorage)
	}
	if len(first.IndexedDB) != 1 || len(first.IndexedDB[0].Stores) != 1 {
		t.Fatalf("IndexedDB = %+v", first.IndexedDB)
	}
	if st := first.IndexedDB[0].Stores[0]; strings.Join(st.IdentifierValues, ",") != "visitor" || st.Entries != 2 {
		t.Errorf("store = %+v, want the visitor field once", st)
	}
	r := &Result{Storage: got}
	origins, third, ids := r.StorageCounts()
	if origins != 3 || third != 2 || ids != 3 { // with no owners known: uid, visitor, pid
		t.Errorf("counts = %d %d %d", origins, third, ids)
	}
}

func TestStorageValuesNeverReachTheResult(t *testing.T) {
	r := &Result{Storage: storage(&capture.Capture{Storage: sampleStorage()}, "shop.test", detect.NewIndex(nil, nil))}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"9f86d081884c7d659a2feaa0c55ad015", "a1b2c3d4e5f6a7b8c9d0e1f2", "3f2b8a4e-9c1d", "5d41402abc4b2a76b9719d911017c592", "dark", "Ada"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the stored value %q is in the result", secret)
		}
	}
	for _, key := range []string{"local_storage", "session_storage", "indexed_db", "cache_storage", "service_workers", "identifier_values"} {
		if !strings.Contains(string(b), key) {
			t.Errorf("the result lacks %s", key)
		}
	}
}

func TestStorageFindings(t *testing.T) {
	r := &Result{Status: StatusDone, Storage: storage(&capture.Capture{Storage: sampleStorage()}, "shop.test", detect.NewIndex(nil, nil))}
	fs := findings(r, detect.Parties{})
	want := map[string]string{"storage": Observed, "third-party-storage": Observed, "storage-identifiers": Classified, "service-workers": Observed}
	for id, label := range want {
		got := find(fs, id)
		if len(got) != 1 || got[0].Label != label {
			t.Errorf("%s = %+v, want one %s finding", id, got, label)
		}
	}
	if f := find(fs, "storage")[0]; !strings.Contains(f.Text, "3 origins kept data") || !strings.Contains(f.Text, "5 local storage keys") ||
		!strings.Contains(f.Text, "1 session storage key") || !strings.Contains(f.Text, "1 IndexedDB database") || !strings.Contains(f.Text, "1 cache") || !strings.Contains(f.Text, "1 service worker") {
		t.Errorf("storage finding = %q", f.Text)
	}
	if f := find(fs, "storage-identifiers")[0]; len(f.Evidence) != 3 || !strings.Contains(strings.Join(f.Evidence, "|"), "IndexedDB app/users visitor") {
		t.Errorf("identifier finding = %+v", f)
	}
	if f := find(fs, "third-party-storage")[0]; len(f.Evidence) != 2 || f.Evidence[0] != "https://ads.partner.test" || f.Evidence[1] != "https://assets.owned.test" {
		t.Errorf("third-party finding = %+v", f)
	}

	quiet := findings(&Result{Status: StatusDone}, detect.Parties{})
	for _, f := range quiet {
		if strings.Contains(f.ID, "storage") || f.ID == "service-workers" {
			t.Errorf("a page with no storage has the finding %+v", f)
		}
	}
}

func TestConditionsSayWhichBrowserWasUsed(t *testing.T) {
	for _, c := range []struct {
		source chrome.Source
		want   string
		pinned bool
	}{
		{chrome.SourcePinned, "pinned", true}, {chrome.SourceSystem, "system", false},
		{chrome.SourceEnv, "env", false}, {chrome.SourceConfig, "config", false}, {"", "unknown", false},
	} {
		r := Build(mustURL(t, "https://example.com/"), &capture.Capture{}, Options{BrowserSource: c.source})
		if r.Conditions.BrowserSource != c.want || r.Conditions.BrowserPinned != c.pinned {
			t.Errorf("%q: source %q pinned %v", c.source, r.Conditions.BrowserSource, r.Conditions.BrowserPinned)
		}
	}
}

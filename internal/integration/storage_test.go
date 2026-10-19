//go:build nosytest

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/scan"
)

func storageOf(r *scan.Result, origin string) *scan.StorageOrigin {
	for i := range r.Storage {
		if r.Storage[i].Origin == origin {
			return &r.Storage[i]
		}
	}
	return nil
}

func keyOf(keys []scan.StorageKey, name string) *scan.StorageKey {
	for i := range keys {
		if keys[i].Key == name {
			return &keys[i]
		}
	}
	return nil
}

func checkStoragePlant(t *testing.T, r *scan.Result) {
	t.Helper()
	if r.Status != scan.StatusDone {
		t.Fatalf("status %s: %s", r.Status, r.Reason)
	}
	if len(r.Storage) != 2 {
		t.Fatalf("storage origins = %+v, want the page and the third-party frame", r.Storage)
	}

	first := storageOf(r, "https://store.test")
	if first == nil || first.ThirdParty {
		t.Fatalf("first-party storage = %+v", first)
	}
	if k := keyOf(first.LocalStorage, "uid"); k == nil || !k.Identifier || k.Length != 32 {
		t.Errorf("localStorage uid = %+v", k)
	}
	if k := keyOf(first.LocalStorage, "theme"); k == nil || k.Identifier {
		t.Errorf("localStorage theme = %+v, want a key that is not an identifier", k)
	}
	if k := keyOf(first.SessionStorage, "tab"); k == nil || k.Identifier {
		t.Errorf("sessionStorage tab = %+v, want listed and never an identifier", k)
	}
	if len(first.IndexedDB) != 1 || first.IndexedDB[0].Name != "appdb" || first.IndexedDB[0].Version != 3 {
		t.Fatalf("IndexedDB = %+v", first.IndexedDB)
	}
	stores := map[string]scan.IDBStore{}
	for _, s := range first.IndexedDB[0].Stores {
		stores[s.Name] = s
	}
	if s := stores["items"]; s.Entries != 2 || strings.Join(s.Keys, ",") != "one,two" || strings.Join(s.IdentifierValues, ",") != "visitor" {
		t.Errorf("store items = %+v", s)
	}
	if s, ok := stores["prefs"]; !ok || s.Entries != 0 {
		t.Errorf("store prefs = %+v, %v", s, ok)
	}
	if len(first.CacheStorage) != 1 || first.CacheStorage[0] != "v1" {
		t.Errorf("caches = %v", first.CacheStorage)
	}
	if len(first.ServiceWorkers) != 1 || first.ServiceWorkers[0].Scope != "https://store.test/" || !strings.HasSuffix(first.ServiceWorkers[0].Script, "/sw.js") {
		t.Errorf("service workers = %+v", first.ServiceWorkers)
	}

	third := storageOf(r, "https://tpstore.test")
	if third == nil || !third.ThirdParty {
		t.Fatalf("third-party storage = %+v", third)
	}
	if k := keyOf(third.LocalStorage, "tpid"); k == nil || !k.Identifier {
		t.Errorf("third-party localStorage = %+v", third.LocalStorage)
	}
	if keyOf(third.SessionStorage, "tpsession") == nil {
		t.Errorf("third-party sessionStorage = %+v", third.SessionStorage)
	}
	if len(third.IndexedDB) != 1 || third.IndexedDB[0].Name != "tpdb" || len(third.IndexedDB[0].Stores) != 1 {
		t.Fatalf("third-party IndexedDB = %+v", third.IndexedDB)
	}
	if s := third.IndexedDB[0].Stores[0]; s.Name != "events" || s.Entries != 1 || strings.Join(s.Keys, ",") != "first" || strings.Join(s.IdentifierValues, ",") != "value" {
		t.Errorf("third-party store = %+v", s)
	}
	if len(third.CacheStorage) != 1 || third.CacheStorage[0] != "tpcache" || len(third.ServiceWorkers) != 0 {
		t.Errorf("third-party caches %v workers %v", third.CacheStorage, third.ServiceWorkers)
	}

	origins, thirdParty, ids := r.StorageCounts()
	// uid, the visitor field, tpid and the third-party record
	if origins != 2 || thirdParty != 1 || ids != 4 {
		t.Errorf("counts = %d origins, %d third-party, %d identifiers", origins, thirdParty, ids)
	}
	want := map[string]string{"storage": scan.Observed, "third-party-storage": scan.Observed, "storage-identifiers": scan.Classified, "service-workers": scan.Observed}
	for id, label := range want {
		found := false
		for _, f := range r.Findings {
			if f.ID == id {
				found = true
				if f.Label != label {
					t.Errorf("%s is %s, want %s", id, f.Label, label)
				}
			}
		}
		if !found {
			t.Errorf("no %s finding in %+v", id, r.Findings)
		}
	}

	// the values themselves are not in what is written out
	b, _ := json.Marshal(r)
	for _, secret := range []string{"9f86d081884c7d659a2feaa0c55ad015", "3f2b8a4e-9c1d-4e7a-b2f0-6d5a1c8e9b73", "5d41402abc4b2a76b9719d911017c592", "b6d767d2f8ed5d21a44b0e5886680cb9", "a1b2c3d4e5f6a7b8c9d0e1f2"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("a stored value is in the result: %s", secret)
		}
	}
	// storage is observation: the score is untouched by it
	if r.Score == nil || r.Score.Total != 0 {
		t.Errorf("score = %+v, want storage not to count", r.Score)
	}
}

func TestStorageIsCapturedPerOrigin(t *testing.T) {
	t.Parallel()
	checkStoragePlant(t, scanTest(t, "https://store.test/"))
}

func TestAPageThatStoresNothingHasNoStorage(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://nostore.test/")
	if r.Status != scan.StatusDone || len(r.Storage) != 0 {
		t.Errorf("status %s storage %+v", r.Status, r.Storage)
	}
	for _, f := range r.Findings {
		if strings.Contains(f.ID, "storage") || f.ID == "service-workers" {
			t.Errorf("finding %+v on a page that stores nothing", f)
		}
	}
	if o, third, ids := r.StorageCounts(); o != 0 || third != 0 || ids != 0 {
		t.Errorf("counts = %d %d %d", o, third, ids)
	}
	// and the other controls are quiet too
	if c := scanTest(t, "http://control.test/"); len(c.Storage) != 0 {
		t.Errorf("control page storage = %+v", c.Storage)
	}
}

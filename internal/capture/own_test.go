package capture

import "testing"

func TestWithoutBrowserOwn(t *testing.T) {
	ntp := "chrome-untrusted://new-tab-page/one-google-bar?paramsencoded="
	bar := "https://www.gstatic.com/og/_/js/k=og.qtm.en_US/rt=j/m=q_dnp"
	reqs := []Request{
		{URL: "https://duckduckgo.com/", Initiator: "other"},
		{URL: "https://www.gstatic.com/images/branding/googlelogo.svg", Initiator: ntp},
		{URL: bar, Initiator: ntp},
		{URL: "https://ogads-pa.clients6.google.com/$rpc/GetAsyncData", Initiator: bar},
		{URL: "https://play.google.com/log?format=json", Initiator: bar},
		{URL: "https://play.google.com/log?format=json", Initiator: "https://play.google.com/log?format=json"},
		{URL: "https://duckduckgo.com/app.js", Initiator: "https://duckduckgo.com/"},
		{URL: "https://www.gstatic.com/og/_/js/page-own.js", Initiator: "https://duckduckgo.com/app.js"},
		{URL: "https://example.com/err", Initiator: "chrome-error://chromewebdata"},
	}
	got := WithoutBrowserOwn(reqs)
	var urls []string
	for _, r := range got {
		urls = append(urls, r.URL)
	}
	want := []string{"https://duckduckgo.com/", "https://duckduckgo.com/app.js", "https://www.gstatic.com/og/_/js/page-own.js"}
	if len(urls) != len(want) {
		t.Fatalf("kept %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Errorf("kept %v, want %v", urls, want)
		}
	}
}

// The chain can be cut: the script that the new tab page loaded was requested
// before the recorder attached, so only its address is left.
func TestBrowserScriptGivesItsRequestsAway(t *testing.T) {
	bar := "https://www.gstatic.com/og/_/js/k=og.qtm.en_US/rt=j/m=q_dnp"
	reqs := []Request{
		{URL: "https://www.alarabiya.net/", Initiator: "other"},
		{URL: "https://play.google.com/log?format=json&hasfast=true", Initiator: bar},
		{URL: "https://play.google.com/log?format=json&hasfast=true", Initiator: "https://play.google.com/log?format=json&hasfast=true"},
		{URL: "https://www.googletagmanager.com/gtag/js?id=G-1", Initiator: "https://www.alarabiya.net/"},
	}
	got := WithoutBrowserOwn(reqs)
	if len(got) != 2 || got[1].URL != "https://www.googletagmanager.com/gtag/js?id=G-1" {
		t.Errorf("kept %+v", got)
	}
}

func TestSiteRequestsAreNeverTheBrowsers(t *testing.T) {
	reqs := []Request{
		{URL: "https://site.test/", Initiator: "other"},
		{URL: "https://cdn.test/a.js", Initiator: "https://site.test/"},
		{URL: "https://track.test/p", Initiator: "https://cdn.test/a.js"},
		{URL: "https://track.test/q", Initiator: "parser"},
	}
	if got := WithoutBrowserOwn(reqs); len(got) != len(reqs) {
		t.Errorf("kept %d of %d requests of a plain page", len(got), len(reqs))
	}
}

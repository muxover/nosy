package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/muxover/nosy/internal/check"
	"github.com/muxover/nosy/internal/scan"
)

const topCompanies = 5

// printOneLine is the --quiet summary.
func printOneLine(w io.Writer, r *scan.Result) {
	switch {
	case r.Score != nil:
		fmt.Fprintf(w, "%s  %d/100  %s\n", r.Site, r.Score.Total, r.Score.Label)
	default:
		fmt.Fprintf(w, "%s  %s: %s\n", r.Site, r.Status, r.Reason)
	}
}

// printSummary is the plain terminal summary. The full reports are step 10.
func printSummary(w io.Writer, r *scan.Result) {
	fmt.Fprintf(w, "%s", r.Site)
	if r.FinalURL != "" {
		fmt.Fprintf(w, "  (%s)", r.FinalURL)
	}
	fmt.Fprint(w, "\n\n")

	if r.Score != nil {
		fmt.Fprintf(w, "Score       %d / 100  %s  (privacy score, formula %s)\n", r.Score.Total, r.Score.Label, r.Score.Formula)
	} else {
		fmt.Fprintf(w, "Status      %s: %s\n", r.Status, r.Reason)
	}
	third, tracking := 0, 0
	for _, c := range r.Companies {
		third++
		if c.Tracking {
			tracking++
		}
	}
	identifiers := 0
	for _, c := range r.Cookies {
		if c.Identifier {
			identifiers++
		}
	}
	fmt.Fprintf(w, "Requests    %d, %d to other companies\n", r.Weight.Requests, r.Weight.ThirdRequests)
	fmt.Fprintf(w, "Companies   %d, %d classified as tracking\n", third, tracking)
	fmt.Fprintf(w, "Cookies     %d, %d look like identifiers\n", len(r.Cookies), identifiers)
	origins, thirdStorage, storedIDs := r.StorageCounts()
	fmt.Fprintf(w, "Storage     %d %s kept data, %d third-party, %d %s like identifiers\n",
		origins, plural(origins, "origin", "origins"), thirdStorage, storedIDs, plural(storedIDs, "value looks", "values look"))

	if len(r.Companies) > 0 {
		fmt.Fprint(w, "\nTop companies\n")
		for i, c := range r.Companies {
			if i == topCompanies {
				fmt.Fprintf(w, "  and %d more\n", len(r.Companies)-topCompanies)
				break
			}
			kind := "other"
			if c.Tracking {
				kind = strings.Join(c.Classes, ", ")
			}
			fmt.Fprintf(w, "  %-28s %-24s %d %s\n", c.Name, kind, c.Requests, plural(c.Requests, "request", "requests"))
		}
	}

	if len(r.Findings) > 0 {
		fmt.Fprint(w, "\nFindings\n")
		for _, f := range r.Findings {
			fmt.Fprintf(w, "  [%s] %s\n", f.Label, f.Text)
		}
	}
	if len(r.Notes) > 0 {
		fmt.Fprint(w, "\nNotes\n")
		for _, n := range r.Notes {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}

	c := r.Conditions
	fmt.Fprintf(w, "\nConditions  %s, %s %s, %s, %.1f s of %d s, Nosy %s, rules %s\n",
		c.Date, c.Device, c.Viewport, c.Browser, float64(c.DurationMS)/1000, c.DurationLimitSeconds, c.Nosy, c.Rules)
	if c.GPC {
		fmt.Fprintln(w, "            Global Privacy Control sent")
	}
}

func printChecks(w io.Writer, r *check.Result) {
	fmt.Fprintf(w, "Checks for %s\n\n", r.URL)
	for _, c := range r.Checks {
		mark := "ok "
		if !c.OK {
			mark = "fix"
		}
		fmt.Fprintf(w, "  %s [%s] %s\n", mark, c.Label, c.Text)
	}
	if len(r.Cookies) > 0 {
		fmt.Fprint(w, "\nCookies in the first answer\n")
		for _, c := range r.Cookies {
			life := "session"
			if !c.Session {
				life = fmt.Sprintf("%d days", c.Days)
			}
			note := ""
			switch {
			case c.Identifier:
				note = "  looks like an identifier"
			case c.AntiBot:
				note = "  bot management"
			}
			fmt.Fprintf(w, "  %-24s %-28s %s%s\n", c.Name, c.Domain, life, note)
		}
	}
	fmt.Fprintf(w, "\nChecked %s, Nosy %s. The setup checks (headers, ads.txt, SPF, DMARC and more) arrive in a later version.\n",
		r.Date, r.Nosy)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

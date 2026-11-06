package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Whether a newer release is out: GitHub's latest release (drafts and
// pre-releases aren't "latest"), compared with this build's version. Nothing
// is downloaded or installed; the page links to the release.

// a var so that a test build can point it elsewhere (-ldflags -X)
var releasesAPI = "https://api.github.com/repos/hwangti/tablume/releases/latest"

const (
	releasesPage = "https://github.com/hwangti/tablume/releases/"
	// An answer is kept this long: every page of the browser build asks, and
	// GitHub allows an address 60 unauthenticated calls an hour.
	updateTTL = 6 * time.Hour
)

type UpdateInfo struct {
	Current string `json:"current"`
	Latest  string `json:"latest"` // the release's version, without a leading v
	URL     string `json:"url"`    // its page
	Newer   bool   `json:"newer"`  // Latest is newer than Current
}

var updates struct {
	mu   sync.Mutex
	at   time.Time
	info UpdateInfo
}

// CheckUpdate asks GitHub for the latest release; force skips the kept answer
// (the "check now" button).
func (a *App) CheckUpdate(force bool) (UpdateInfo, error) {
	updates.mu.Lock()
	defer updates.mu.Unlock()
	if !force && !updates.at.IsZero() && time.Since(updates.at) < updateTTL {
		return updates.info, nil
	}
	info, err := latestRelease(version)
	if err != nil {
		return UpdateInfo{Current: version}, err
	}
	updates.at, updates.info = time.Now(), info
	return info, nil
}

func latestRelease(current string) (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return UpdateInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Tablume/"+current)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return UpdateInfo{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound { // no release yet
		return UpdateInfo{Current: current}, nil
	}
	if res.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("GitHub: %s", res.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return UpdateInfo{}, err
	}
	latest := strings.TrimPrefix(r.Tag, "v")
	if _, ok := parseVersion(latest); !ok {
		return UpdateInfo{}, errors.New("GitHub: unexpected release tag " + strconv.Quote(r.Tag))
	}
	// only ever a page of this project's releases
	url := r.URL
	if !strings.HasPrefix(url, releasesPage) {
		url = releasesPage + "latest"
	}
	return UpdateInfo{Current: current, Latest: latest, URL: url, Newer: newerVersion(latest, current)}, nil
}

// OpenUpdatePage opens the release page found by CheckUpdate in the browser
// (desktop app; the browser build opens it itself).
func (a *App) OpenUpdatePage() error {
	updates.mu.Lock()
	url := updates.info.URL
	updates.mu.Unlock()
	if url == "" {
		url = releasesPage + "latest"
	}
	return openURL(url)
}

// x.y.z with an optional -pre part; a build that isn't one (a dev build) is
// never older than anything.
type semver struct {
	n   [3]int
	pre string
}

func parseVersion(s string) (semver, bool) {
	var v semver
	s, v.pre, _ = strings.Cut(strings.TrimPrefix(s, "v"), "-")
	s, _, _ = strings.Cut(s, "+")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.n[i] = n
	}
	return v, true
}

// newerVersion: latest is a later release than current. 1.0.0-rc1 comes
// before 1.0.0; two pre-releases of one version compare as text.
func newerVersion(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l.n {
		if l.n[i] != c.n[i] {
			return l.n[i] > c.n[i]
		}
	}
	switch {
	case l.pre == c.pre:
		return false
	case l.pre == "":
		return true
	case c.pre == "":
		return false
	}
	return l.pre > c.pre
}

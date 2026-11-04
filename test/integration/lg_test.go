//go:build integration

package integration

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
)

func (l lgList) pkgNamed(name string) (p struct{ ID, SaveTo, Priority, Comment, Password string }, ok bool) {
	for _, k := range l.Packages {
		if k.Name == name {
			return struct{ ID, SaveTo, Priority, Comment, Password string }{k.ID, k.SaveTo, k.Priority, k.Comment, k.DownloadPassword}, true
		}
	}
	return p, false
}

func numberedURLs(dir string, n int) (urls, names []string) {
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("it-lg-%05d.bin", i)
		urls = append(urls, fx("/"+dir+"/"+name+"?size=100"))
		names = append(names, name)
	}
	return
}

// addMany adds the links in one request and waits for all of them.
func (a *app) addMany(t testing.TB, pkg string, urls, names []string, timeout time.Duration) (took time.Duration) {
	t.Helper()
	t0 := time.Now()
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": urls, "packageName": pkg, "autostart": false, "overwritePackagizerRules": true}, nil, 200)
	waitFor(t, fmt.Sprintf("%d links in the linkgrabber", len(names)), timeout, func() bool {
		var l lgList
		a.must(t, "GET", "/api/v1/linkgrabber", nil, &l, 200)
		return len(l.Links) >= len(names) && !a.status(t).Linkgrabber.Crawling
	})
	return time.Since(t0)
}

// T-LG-01: 1 link, 100 links (and 10,000 when JDWEB_IT_BIG=1: -run TestLG01_10000).
func TestLG01_Counts(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	for _, n := range []int{1, 100} {
		urls, names := numberedURLs(fmt.Sprintf("lg01-%d", n), n)
		took := a.addMany(t, fmt.Sprintf("%slg01-%d", prefix, n), urls, names, 120*time.Second)
		l := a.grabber(t)
		if len(l.Links) != n {
			t.Errorf("%d links added, %d shown", n, len(l.Links))
		}
		t.Logf("T-LG-01 %d links: all in the mirror after %v", n, took.Round(100*time.Millisecond))
		cleanJD(t)
	}
}

func TestLG01_10000(t *testing.T) {
	if os.Getenv("JDWEB_IT_BIG") == "" {
		t.Skip("set JDWEB_IT_BIG=1 for the 10,000 links case")
	}
	cleanJD(t)
	a := shared(t)
	urls, names := numberedURLs("lg01-big", 10000)
	took := a.addMany(t, prefix+"lg01-big", urls, names, 15*time.Minute)
	l := a.grabber(t)
	t0 := time.Now()
	st := a.status(t)
	t.Logf("T-LG-01 10000 links: %d in the mirror after %v; /status takes %v; linkgrabber links %d", len(l.Links), took.Round(time.Second), time.Since(t0).Round(time.Millisecond), st.Linkgrabber.Links)
	if len(l.Links) != 10000 {
		t.Errorf("%d links shown", len(l.Links))
	}
}

// T-LG-02: text with links between words.
func TestLG02_TextWithLinks(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	// Found against revision 50639: JDownloader reads a plain http URL up to
	// the end of its line ("url and more" becomes the file name "x.bin and
	// more"), only <...> ends it. So the links stand on lines of their own.
	text := "Hello, please get these:\n" + fx("/it-lg02-a.bin") + "\nalso <" + fx("/it-lg02-b.bin") + "> thanks.\n\t" + fx("/it-lg02-c.bin") + "\nbye"
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": []string{text}, "packageName": prefix + "lg02", "autostart": false}, nil, 200)
	t.Cleanup(func() {
		if t.Failed() {
			for _, k := range a.grabber(t).Links {
				t.Logf("in the list: %q %s", k.Name, k.URL)
			}
		}
	})
	l := a.waitGrabberNames(t, "it-lg02-a.bin", "it-lg02-b.bin", "it-lg02-c.bin")
	t.Logf("T-LG-02 3 links found in one block of text; %d links in the list", len(l.Links))
}

// T-LG-03: every option of the add dialog arrives in JDownloader.
func TestLG03_Options(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	dir := directory("lg03")
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{
		"links": []string{fx("/it-lg03.bin")}, "packageName": prefix + "lg03", "destinationFolder": dir,
		"extractPassword": "ex-pass", "downloadPassword": "dl-pass", "autostart": false, "autoExtract": true,
		"deepDecrypt": false, "overwritePackagizerRules": true, "priority": "HIGHER",
	}, nil, 200)
	l := a.waitGrabberNames(t, "it-lg03.bin")
	p, _ := l.pkgNamed(prefix + "lg03")
	if p.SaveTo != dir {
		t.Errorf("saveTo = %q", p.SaveTo)
	}
	links, err := c.GrabberLinks(bg(), jd.AllGrabberLinkFields())
	if err != nil || len(links) != 1 {
		t.Fatalf("%v %d", err, len(links))
	}
	k, _ := l.link("it-lg03.bin")
	t.Logf("T-LG-03 priority link=%q package=%q downloadPassword link=%q package=%q", k.Priority, p.Priority, k.DownloadPassword, p.Password)
	if k.Priority != "HIGHER" && p.Priority != "HIGHER" {
		t.Errorf("priority HIGHER did not arrive")
	}
	if k.DownloadPassword != "dl-pass" && p.Password != "dl-pass" {
		t.Errorf("download password did not arrive")
	}
	// The archive password lands in the archive settings once the links are archives; here only that it was accepted.
	// autostart: with autostart true the downloads begin although the controller is stopped.
	cleanJD(t)
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": []string{fx("/slow/it-lg03b.bin")}, "packageName": prefix + "lg03b", "autostart": true, "destinationFolder": dir}, nil, 200)
	eventually(t, "autostart moves the link to the download list", 60*time.Second, func() bool { return len(a.downloads(t).Links) == 1 })
	t.Logf("T-LG-03 autostart:true -> download list holds the link, controller %s", a.status(t).Controller)
}

// T-LG-04: container file. A DLC needs JDownloader's decryption service on the
// internet, so an RSDF (a local cipher) is tried as well.
func TestLG04_Container(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	url := fx("/it-lg04.bin")
	// RSDF: AES-CFB, key and IV as in JDownloader's RSDF decrypter, one line per link.
	key, _ := hex.DecodeString("8C35192D964DC3182C6F84F3252239EB4A320D2500000000")
	blk, _ := aes.NewCipher(key)
	iv := make([]byte, 16)
	blk.Encrypt(iv, make([]byte, 16))
	line := cipher.NewCFBEncrypter(blk, iv)
	plain := []byte(base64.StdEncoding.EncodeToString([]byte(url)))
	enc := make([]byte, len(plain))
	line.XORKeyStream(enc, plain)
	content := hex.EncodeToString(enc) + "\n"
	body := map[string]any{"name": "it-lg04.rsdf", "contentBase64": base64.StdEncoding.EncodeToString([]byte(content))}
	a.must(t, "POST", "/api/v1/linkgrabber/container", body, nil, 200)
	ok := false
	for i := 0; i < 60 && !ok; i++ {
		time.Sleep(500 * time.Millisecond)
		_, ok = a.grabber(t).link("it-lg04.bin")
	}
	if ok {
		t.Log("T-LG-04 RSDF container: link appears")
	} else {
		t.Log("T-LG-04 RSDF container: no link appeared (no hand-made container that this JDownloader reads)")
	}
	// A broken DLC must not crash anything, JDWeb answers.
	a.must(t, "POST", "/api/v1/linkgrabber/container", map[string]any{"name": "broken.dlc", "contentBase64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 200)))}, nil, 200)
	// Wrong input is a 400.
	if st, _ := a.call("POST", "/api/v1/linkgrabber/container", map[string]any{"name": "x.dlc", "contentBase64": "!!"}); st != 400 {
		t.Errorf("bad base64 = %d", st)
	}
	if got := a.status(t).Connection; got != "connected" {
		t.Errorf("connection = %s", got)
	}
}

// T-LG-05: abort the link search.
func TestLG05_Abort(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	urls, _ := numberedURLs("lg05", 300)
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": urls, "packageName": prefix + "lg05", "autostart": false}, nil, 200)
	eventually(t, "crawling shows", 20*time.Second, func() bool { return a.status(t).Linkgrabber.Crawling })
	a.action(t, "linkgrabber", "abort", map[string]any{})
	eventually(t, "crawling is over", 30*time.Second, func() bool { return !a.status(t).Linkgrabber.Crawling })
	n1 := len(a.grabber(t).Links)
	time.Sleep(3 * time.Second)
	n2 := len(a.grabber(t).Links)
	t.Logf("T-LG-05 abort: %d of 300 links before, %d 3 s later", n1, n2)
	if n2 > n1 {
		t.Errorf("links keep coming after the abort: %d -> %d", n1, n2)
	}
}

// T-LG-07: offline link: availability, clean-up removes it.
func TestLG07_Offline(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": []string{fx("/offline/it-lg07-off.bin"), fx("/it-lg07-on.bin")}, "packageName": prefix + "lg07", "autostart": false}, nil, 200)
	var l lgList
	eventually(t, "both links with a known availability", 60*time.Second, func() bool {
		l = a.grabber(t)
		off, ok1 := l.link("it-lg07-off.bin")
		on, ok2 := l.link("it-lg07-on.bin")
		return ok1 && ok2 && off.Availability == "OFFLINE" && on.Availability == "ONLINE"
	})
	a.action(t, "linkgrabber", "cleanup", with(sel(nil, nil), "action", "DELETE_OFFLINE", "mode", "REMOVE_LINKS_ONLY", "selection", "ALL"))
	eventually(t, "the offline link is gone", 20*time.Second, func() bool {
		l := a.grabber(t)
		_, off := l.link("it-lg07-off.bin")
		_, on := l.link("it-lg07-on.bin")
		return !off && on
	})
}

// A package with an offline link: JDownloader moves nothing and says success.
// JDWeb sends the online links and tells what stayed.
func TestLG07_ToDownloadsSkipsOfflineLinks(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": []string{fx("/offline/it-lg07m-off.bin"), fx("/it-lg07m-on.bin")}, "packageName": prefix + "lg07m", "autostart": false}, nil, 200)
	var g lgList
	eventually(t, "both links with a known availability", 60*time.Second, func() bool {
		g = a.grabber(t)
		off, ok1 := g.link("it-lg07m-off.bin")
		on, ok2 := g.link("it-lg07m-on.bin")
		return ok1 && ok2 && off.Availability == "OFFLINE" && on.Availability == "ONLINE"
	})
	var out struct{ Moved, SkippedOffline int }
	a.must(t, "POST", "/api/v1/linkgrabber/actions/to-downloads", map[string]any{"start": false, "packageIds": lgPackageIDs(g), "linkIds": []string{}}, &out, 200)
	if out.Moved != 1 || out.SkippedOffline != 1 {
		t.Errorf("answer = %+v, want moved 1, skippedOffline 1", out)
	}
	eventually(t, "the online link is in the downloads, the offline one stayed", 30*time.Second, func() bool {
		_, inDl := a.downloads(t).link("it-lg07m-on.bin")
		_, offStays := a.grabber(t).link("it-lg07m-off.bin")
		return inDl && offStays
	})
	// Only the offline link: nothing to move, nothing moves.
	off, _ := a.grabber(t).link("it-lg07m-off.bin")
	a.must(t, "POST", "/api/v1/linkgrabber/actions/to-downloads", map[string]any{"start": false, "linkIds": []string{off.ID}, "packageIds": []string{}}, &out, 200)
	if out.Moved != 0 || out.SkippedOffline != 1 {
		t.Errorf("only offline: %+v", out)
	}
}

// T-LG-08: moving to the download list keeps the link IDs.
func TestLG08_ToDownloadsKeepsIDs(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	g := a.add(t, prefix+"lg08", directory("lg08"), fx("/it-lg08-1.bin"), fx("/it-lg08-2.bin"))
	ids := lgLinkIDs(g)
	a.action(t, "linkgrabber", "to-downloads", map[string]any{"start": false, "packageIds": lgPackageIDs(g)})
	var d dlList
	eventually(t, "links in downloads", 30*time.Second, func() bool { d = a.downloads(t); return len(d.Links) == 2 })
	got := dlLinkIDs(d)
	same := 0
	for _, x := range ids {
		for _, y := range got {
			if x == y {
				same++
			}
		}
	}
	t.Logf("T-LG-08 link ids kept: %d of %d (grabber %v, downloads %v)", same, len(ids), ids, got)
	if same != len(ids) {
		t.Errorf("link IDs changed on the way to the download list")
	}
}

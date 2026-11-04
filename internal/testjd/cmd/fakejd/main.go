// Command fakejd runs the fake JDownloader of internal/testjd on a fixed port
// with a realistic seed, so that the web interface can be developed and
// photographed without a real JDownloader. It is a development tool and is not
// part of the image.
//
//	go run ./internal/testjd/cmd/fakejd -addr 127.0.0.1:3128
//	go run ./internal/testjd/cmd/fakejd -links 10000
//
// A small control server (-ctl) simulates what a browser test needs:
// GET /down and /up (JDownloader restarts), /captcha, /add (a package in the
// linkgrabber), /package and /many (more download links), /state (a line of text).
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/Buffer379x/jdweb/internal/testjd"
)

const mb = 1 << 20

func main() {
	addr := flag.String("addr", "127.0.0.1:3128", "address of the fake JDownloader API")
	ctl := flag.String("ctl", "127.0.0.1:3129", "address of the control server, empty for none")
	many := flag.Int("links", 0, "instead of the small seed: this many links in packages of 100")
	generated := flag.Int("config", 0, "this many generated settings on top of the seed")
	tick := flag.Duration("tick", time.Second, "how often running downloads advance, 0 for never")
	flag.Parse()

	s, err := testjd.StartOn(*addr)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if *many > 0 {
		seedMany(s, *many)
	} else {
		seed(s)
	}
	seedExtras(s, *generated)
	s.SetController("RUNNING")
	if *tick > 0 {
		go advance(s, *tick)
	}
	if *ctl != "" {
		go serveControl(s, *ctl)
	}
	fmt.Println("fake JDownloader on", s.URL)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func link(name, host string, total, loaded int64, extra func(*testjd.Link)) testjd.Link {
	l := testjd.Link{Name: name, URL: "https://" + host + "/file/" + name, Host: host, BytesTotal: total, BytesLoaded: loaded}
	if extra != nil {
		extra(&l)
	}
	return l
}

func running(speed int64) func(*testjd.Link) {
	return func(l *testjd.Link) { l.Running, l.Speed, l.Status = true, speed, "Download" }
}
func finished(l *testjd.Link) {
	l.Finished, l.BytesLoaded, l.Status = true, l.BytesTotal, "Finished"
}

func seed(s *testjd.Server) {
	d := testjd.Downloads
	s.AddPackage(d, testjd.Package{Name: "Big Buck Bunny 4K", SaveTo: "/mnt/downloads/Big Buck Bunny 4K"},
		link("bunny-2160p60.mp4", "youtube.com", 1340*mb, 830*mb, running(7800000)),
		link("bunny-1080p30.mp4", "youtube.com", 620*mb, 120*mb, running(4600000)),
		link("bunny-audio-aac.m4a", "youtube.com", 10*mb, 10*mb, finished),
		link("bunny-subtitles.srt", "youtube.com", 64<<10, 64<<10, finished),
		link("bunny-poster.jpg", "youtube.com", 2*mb, 0, nil),
	)
	s.AddPackage(d, testjd.Package{Name: "Debian 13 Installationsmedien", SaveTo: "/mnt/downloads/debian"},
		link("debian-13-amd64-netinst.iso", "debian.org", 750*mb, 750*mb, finished),
		link("debian-13-amd64-DVD-1.iso", "debian.org", 3900*mb, 1560*mb, func(l *testjd.Link) { l.Status = "Waiting for slot" }),
		link("debian-13-arm64-DVD-1.iso", "debian.org", 3800*mb, 0, nil),
		link("SHA256SUMS", "debian.org", 4<<10, 0, func(l *testjd.Link) { l.Disabled = true }),
	)
	s.AddPackage(d, testjd.Package{Name: "Projektarchiv 2025", SaveTo: "/mnt/downloads/archiv"},
		link("archiv.part1.rar", "debrid.example", 800*mb, 800*mb, finished),
		link("archiv.part2.rar", "debrid.example", 800*mb, 800*mb, finished),
		link("archiv.part3.rar", "debrid.example", 800*mb, 800*mb, func(l *testjd.Link) {
			finished(l)
			l.ExtractionStatus = "ERROR_CRC"
		}),
		link("archiv.part4.rar", "debrid.example", 400*mb, 400*mb, finished),
		link("archiv.part5.rar", "debrid.example", 300*mb, 300*mb, finished),
		link("archiv.part6.rar", "debrid.example", 100*mb, 0, func(l *testjd.Link) { l.Skipped, l.SkipReason = true, "DISK_FULL" }),
		link("archiv.part7.rar", "debrid.example", 100*mb, 0, func(l *testjd.Link) { l.FinalState, l.Status = "OFFLINE", "File not found" }),
	)
	long := strings.Repeat("sehr-langer-dateiname-", 22) + ".bin"
	s.AddPackage(d, testjd.Package{Name: "Feindliche Namen", SaveTo: "/mnt/downloads/hostile"},
		link("<script>alert(1)</script>.txt", "example.org", 5*mb, 0, nil),
		link("‮txt.exe-gnp.‭", "example.org", 5*mb, 0, nil),
		link("ملف عربي طويل.pdf", "example.org", 5*mb, 0, nil),
		link("emoji-\U0001F3AC\U0001F4A5-clip.mkv", "example.org", 80*mb, 10*mb, nil),
		link(long, "example.org", 1*mb, 0, nil),
	)
	var many []testjd.Link
	for i := 1; i <= 40; i++ {
		l := link(fmt.Sprintf("folge-%02d.mkv", i), "archive.org", 300*mb, 0, nil)
		switch {
		case i <= 12:
			finished(&l)
		case i == 13:
			running(3100000)(&l)
			l.BytesLoaded = 90 * mb
		}
		many = append(many, l)
	}
	s.AddPackage(d, testjd.Package{Name: "Serie Staffel 1 (40 Folgen)", SaveTo: "/mnt/downloads/serie"}, many...)

	g := testjd.Linkgrabber
	variants := []testjd.Variant{
		{ID: "av1-2160", Name: "2160p AV1", IconKey: "video"}, {ID: "vp9-1440", Name: "1440p VP9", IconKey: "video"},
		{ID: "h264-1080", Name: "1080p H.264", IconKey: "video"}, {ID: "h264-720", Name: "720p H.264", IconKey: "video"},
		{ID: "opus", Name: "Audio Opus", IconKey: "audio"}, {ID: "aac", Name: "Audio AAC", IconKey: "audio"}, {ID: "mp3", Name: "Audio MP3", IconKey: "audio"},
	}
	s.AddPackage(g, testjd.Package{Name: "Sintel Open Movie", SaveTo: "/mnt/downloads/Sintel"},
		link("sintel (2160p, AV1).mp4", "youtube.com", 1980*mb, 0, func(l *testjd.Link) { l.Variants, l.VariantID = variants, "av1-2160" }),
		link("sintel-trailer.mp4", "youtube.com", 60*mb, 0, nil),
		link("sintel-soundtrack.m4a", "youtube.com", 20*mb, 0, nil),
		link("sintel-making-of.mp4", "youtube.com", 120*mb, 0, nil),
	)
	s.AddPackage(g, testjd.Package{Name: "Sammlung Handbücher", SaveTo: "/mnt/downloads/docs"},
		link("handbuch-1.pdf", "archive.org", 12*mb, 0, nil), link("handbuch-2.pdf", "archive.org", 9*mb, 0, nil),
		link("handbuch-3.pdf", "archive.org", 14*mb, 0, nil), link("handbuch-4.pdf", "mediafire.com", 11*mb, 0, nil),
		link("handbuch-5.pdf", "mediafire.com", 18*mb, 0, nil), link("handbuch-6.pdf", "mediafire.com", 7*mb, 0, nil),
		link("handbuch-7.pdf", "mediafire.com", 13*mb, 0, func(l *testjd.Link) { l.Availability = "OFFLINE" }),
	)
	s.AddPackage(g, testjd.Package{Name: "Unbekannte Quelle", SaveTo: "/mnt/downloads/misc"},
		link("datei-a.zip", "example.org", 0, 0, func(l *testjd.Link) { l.Availability = "UNKNOWN" }),
		link("datei-b.zip", "example.org", 0, 0, func(l *testjd.Link) { l.Availability = "TEMP_UNKNOWN" }),
	)
	s.AddCaptcha(imageCaptcha("example.org", "W7K3Q", 4*time.Minute))
}

func seedMany(s *testjd.Server, n int) {
	addMany(s, n)
	s.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "Gesammelt", SaveTo: "/mnt/downloads/gesammelt"},
		link("gesammelt-1.bin", "example.org", 10*mb, 0, nil), link("gesammelt-2.bin", "example.org", 10*mb, 0, nil))
}

// addMany adds n download links in packages of 100, in all states.
func addMany(s *testjd.Server, n int) {
	hosts := []string{"youtube.com", "debian.org", "archive.org", "mediafire.com", "example.org"}
	rng := rand.New(rand.NewSource(1))
	for p := 0; p*100 < n; p++ {
		count := min(100, n-p*100)
		links := make([]testjd.Link, count)
		for i := range links {
			total := int64(rng.Intn(900)+1) * mb
			l := link(fmt.Sprintf("paket-%03d-datei-%03d.bin", p, i), hosts[rng.Intn(len(hosts))], total, 0, nil)
			switch r := rng.Intn(100); {
			case r < 40:
				finished(&l)
			case r < 46:
				running(int64(rng.Intn(9000000) + 100000))(&l)
				l.BytesLoaded = total / 3
			case r < 48:
				l.ExtractionStatus = "ERROR_CRC"
				finished(&l)
			case r < 50:
				l.Skipped, l.SkipReason = true, "DISK_FULL"
			case r < 51:
				l.FinalState, l.Status = "OFFLINE", "File not found"
			case r < 52:
				l.Disabled = true
			}
			links[i] = l
		}
		s.AddPackage(testjd.Downloads, testjd.Package{Name: fmt.Sprintf("Paket %03d", p), SaveTo: fmt.Sprintf("/mnt/downloads/paket-%03d", p)}, links...)
	}
}

// addPackage adds one download package of n links: 40 % finished, 10 % running, 5 % failed, the rest waiting.
func addPackage(s *testjd.Server, name string, n int) {
	links := make([]testjd.Link, n)
	for i := range links {
		l := link(fmt.Sprintf("%s-%04d.bin", name, i), "example.org", 100*mb, 0, nil)
		switch r := i * 100 / max(n, 1); {
		case r < 40:
			finished(&l)
		case r < 50:
			running(1 * mb)(&l)
			l.BytesLoaded = 30 * mb
		case r < 55:
			l.ExtractionStatus = "ERROR_CRC"
			finished(&l)
		}
		links[i] = l
	}
	s.AddPackage(testjd.Downloads, testjd.Package{Name: name, SaveTo: "/mnt/downloads/" + name}, links...)
}

// advance moves the running downloads forward and tells JDownloader's
// listeners, as the real one would. A download that gets close to its end
// starts over at a third, so that a screenshot session always has something running.
func advance(s *testjd.Server, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		for _, l := range s.Links(testjd.Downloads) {
			if !l.Running || l.BytesTotal == 0 {
				continue
			}
			id := l.UUID
			var loaded int64
			s.UpdateLink(testjd.Downloads, id, func(x *testjd.Link) {
				x.BytesLoaded += x.Speed / 8
				if x.BytesLoaded >= x.BytesTotal*95/100 {
					x.BytesLoaded = x.BytesTotal / 3
				}
				loaded = x.BytesLoaded
			})
			s.Emit(testjd.Event{Publisher: "downloads", EventID: "LINK_UPDATE.bytesLoaded", EventData: map[string]any{"uuid": id, "bytesLoaded": loaded}})
		}
	}
}

func serveControl(s *testjd.Server, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/down", func(w http.ResponseWriter, _ *http.Request) { s.Down(); fmt.Fprintln(w, "down") })
	mux.HandleFunc("/up", func(w http.ResponseWriter, _ *http.Request) { s.Up(); fmt.Fprintln(w, "up") })
	mux.HandleFunc("/captcha", func(w http.ResponseWriter, _ *http.Request) {
		s.AddCaptcha(imageCaptcha("example.org", "X9P2M", 2*time.Minute))
		fmt.Fprintln(w, "captcha")
	})
	mux.HandleFunc("/captcha-interactive", func(w http.ResponseWriter, _ *http.Request) {
		s.AddCaptcha(interactiveCaptcha("rapidgator.net", 3*time.Minute))
		fmt.Fprintln(w, "interactive captcha")
	})
	mux.HandleFunc("/dialogs", func(w http.ResponseWriter, _ *http.Request) {
		addDialogs(s)
		fmt.Fprintln(w, "dialogs")
	})
	// /package?name=X&links=N: one more download package. /many?links=N: N more links in packages of 100.
	mux.HandleFunc("/package", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("links"))
		addPackage(s, r.URL.Query().Get("name"), n)
		fmt.Fprintln(w, "package")
	})
	mux.HandleFunc("/many", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("links"))
		addMany(s, n)
		fmt.Fprintln(w, "many")
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, s.Controller(), len(s.Links(testjd.Downloads)), len(s.Links(testjd.Linkgrabber)))
	})
	log.Println(http.ListenAndServe(addr, mux))
}

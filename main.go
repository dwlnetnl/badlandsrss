// Command badlandsrss fetches the Badlands Media podcast
// feed to split it up and host a per-show feeds.
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"iter"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

func main() {
	addr := flag.String("addr", ":52390", "address to serve on")
	feed := flag.String("url", "https://feed.podbean.com/badlandsmedia/feed.xml", "feed to fetch")
	refresh := flag.Duration("refresh", 5*time.Minute, "feed refresh interval")
	debug := flag.Bool("debug", false, "enable debugging")
	flag.Parse()

	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	ctx := context.Background()
	feeds := &feeds{
		active: make(map[string]*showFeed),
		feed:   *feed,
		log:    slog.Default(),
	}
	srv := &http.Server{
		Addr:        *addr,
		Handler:     feeds,
		ReadTimeout: 5 * time.Second,
	}

	go feeds.Run(ctx, *refresh)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("http server listen:", err)
		}
	}()

	<-ctx.Done()
	log.Println("context done:", ctx.Err())

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("http server shutdown:", err)
	}
}

type feeds struct {
	mu     sync.RWMutex // protects active
	active map[string]*showFeed
	feed   string
	log    *slog.Logger
}

func (f *feeds) Run(ctx context.Context, every time.Duration) {
	tick := time.Tick(every)

	for {
		err := f.updateFeed(ctx, f.feed)
		if err == nil {
			break
		}
		f.log.Error("feed fetch failed", "feed", f.feed, "first", true, "err", err)
		<-tick
	}

	for range tick {
		err := f.fetchFeed(ctx)
		if err != nil {
			f.log.Error("feed fetch failed", "feed", f.feed, "first", false, "err", err)
		}
	}
}

func (f *feeds) fetchFeed(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return f.updateFeed(ctx, f.feed)
}

func (f *feeds) updateFeed(ctx context.Context, feed string) error {
	f.log.Debug("read feed", "feed", feed)
	data, err := readFeed(ctx, feed)
	if err != nil {
		return fmt.Errorf("error reading feed: %w", err)
	}

	p := &parser{data: data}
	prelude := p.Prelude()
	postlude := p.Postlude()
	showItems := make(map[string][][]byte)
	for item := range p.Items() {
		show := p.ShowTitle()
		showItems[show] = append(showItems[show], item)
	}
	if err := p.Err(); err != nil {
		return fmt.Errorf("error parsing feed: %w", err)
	}

	feeds := make(map[string]*showFeed)
	for show, items := range showItems {
		sysName := showSysName(show)
		feeds[sysName] = newShowFeed(show, sysName, prelude, items, postlude)
		f.log.Debug("found", "show", show, "sys", sysName)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// TODO: make per-show feeds persistent and also merge in
	// new and updated shows and episodes just parsed in feed

	f.active = feeds
	return nil
}

func (f *feeds) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := f.log.With("method", r.Method, "path", r.URL.Path)
	defer func() { log.Info("http request") }()

	if r.Method != "GET" && r.Method != "HEAD" {
		log = log.With("outcome", "method not allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f.mu.RLock()
	active := f.active
	f.mu.RUnlock()

	if r.URL.Path == "/" {
		log = log.With("outcome", "render index template")
		shows := slices.Sorted(maps.Keys(active))
		if err := indexTmpl.Execute(w, shows); err != nil {
			log = log.With("err", err)
		}
		return
	}

	if len(active) == 0 {
		log = log.With("outcome", "service unavailable")
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	filename, _ := strings.CutPrefix(r.URL.Path, "/")
	show, _ := strings.CutSuffix(filename, ".xml")
	feed := active[show]
	if feed == nil {
		log = log.With("outcome", "feed not found", "show", show)
		http.NotFound(w, r)
		return
	}

	log = log.With("outcome", "render show feed", "show", show)
	w.Header().Set("Content-Type", "text/xml")
	http.ServeContent(w, r, feed.FileName(), feed.PubDate(), feed.ReadSeeker())
}

var indexTmpl = template.Must(template.New("index").Parse(
	`<html>
<body>
<ul>{{range $show := .}}
	<li><a href="/{{ $show }}.xml">{{ $show }}</a></li>{{end}}
</ul>
</body>
</html>
`))

func readFeed(ctx context.Context, feed string) ([]byte, error) {
	u, err := url.Parse(feed)
	if err != nil {
		return nil, fmt.Errorf("invalid feed: %w", err)
	}

	var rc io.ReadCloser
	switch u.Scheme {
	case "http", "https":
		req := &http.Request{
			Method: "GET",
			URL:    u,
		}
		req = req.WithContext(ctx)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch: %w", err)
		}
		rc = resp.Body

	case "file":
		file := strings.TrimPrefix(feed, "file://")
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("open: %w", err)
		}
		rc = f
	}

	defer rc.Close()
	return io.ReadAll(rc)
}

type parser struct {
	data []byte
	off  int
	err  error
	item []byte
}

var (
	itemBegin  = []byte("<item>")
	itemEnd    = []byte("</item>")
	titleBegin = []byte("<title>")
	titleEnd   = []byte("</title>")
)

var errCorruptFeed = errors.New("corrupt feed")

func (p *parser) Prelude() []byte {
	if p.err != nil {
		return nil
	}
	i := bytes.Index(p.data, itemBegin)
	if i == -1 {
		p.err = errCorruptFeed
		return nil
	}
	// elide newline at end of prelude, everything
	// else is expected to start with a newline
	j := bytes.LastIndexByte(p.data[:i], '>')
	if j == -1 {
		p.err = errCorruptFeed
		return nil
	}
	return p.data[:j+1]
}

func (p *parser) Postlude() []byte {
	if p.err != nil {
		return nil
	}
	i := bytes.LastIndex(p.data, itemEnd)
	if i == -1 {
		p.err = errCorruptFeed
		return nil
	}
	return p.data[i+len(itemEnd):]
}

func (p *parser) Items() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		i := bytes.Index(p.data, itemBegin)
		j := bytes.LastIndexByte(p.data[:i], '>')
		if i == -1 || j == -1 {
			// check after both lookups is okay
			// because feed is likely to be valid
			p.err = errCorruptFeed
			return
		}

		p.off = j + 1
		for {
			i := bytes.Index(p.data[p.off:], itemEnd)
			if i == -1 {
				j := bytes.LastIndex(p.data, itemEnd)
				if j == -1 || p.off != j+len(itemEnd) {
					p.err = errCorruptFeed
				}
				return
			}

			end := p.off + i + len(itemEnd)
			p.item = p.data[p.off:end]
			if !yield(p.item) {
				return
			}

			p.off = end
		}
	}
}

func (p *parser) ShowTitle() string {
	if p.err != nil {
		return ""
	}

	off, end := findByteRange(p.item, titleBegin, titleEnd)
	if off == -1 || end == -1 {
		p.err = errCorruptFeed
		return ""
	}

	title := string(p.item[off:end])
	show := showTitle(title)

	// package encoding/xml only escapes text
	// package html seems close enough to XML
	return html.UnescapeString(show)
}

func (p *parser) Err() error {
	return p.err
}

func showTitle(title string) string {
	if strings.HasPrefix(title, "Altered State") {
		return "Altered State"
	}
	if wwg1wgaRegexp.MatchString(title) {
		return "WWG1WGA After Dark"
	}
	if yChromesRegexp.MatchString(title) {
		return "Y-Chromes"
	}

	matches := episodeRegexp.FindStringSubmatch(title)
	if matches != nil {
		return matches[1]
	}

	return ""
}

var (
	episodeRegexp  = regexp.MustCompile(`(.*?)(?:,? Ep.? \d+(?: -|:)|:| - Chapter \d+:) .*`)
	wwg1wgaRegexp  = regexp.MustCompile(`WWG1WGA(?: After Dark Ep. \d+:|: After Dark Ep. \d+ –) .*`)
	yChromesRegexp = regexp.MustCompile(`Y[- ]Chromes Ep. \d+: .*`)
)

func showSysName(title string) string {
	escaped := false
	escape := func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if escaped {
				return -1
			}
			escaped = true
			return '-'
		}
		escaped = false
		return unicode.ToLower(r)
	}
	return strings.Map(escape, title)
}

type showFeed struct {
	sysName string
	pubDate time.Time
	data    []byte
}

func newShowFeed(show, sysName string, prelude []byte, items [][]byte, postlude []byte) *showFeed {
	edits := fixShowTitle(prelude, show)
	if e, ok := markFeedPrivate(prelude); ok {
		edits = append(edits, e)
	}

	prelude = applyEdits(prelude, edits)
	return &showFeed{
		sysName: sysName,
		pubDate: pubDateOrNow(prelude),
		data:    concatFeedData(prelude, items, postlude),
	}
}

func concatFeedData(prelude []byte, items [][]byte, postlude []byte) []byte {
	size := len(prelude)
	size += len(postlude)
	for _, ep := range items {
		size += len(ep)
	}

	buf := make([]byte, 0, size)
	buf = append(buf, prelude...)
	for _, item := range items {
		buf = append(buf, item...)
	}
	buf = append(buf, postlude...)
	return buf
}

func (sf *showFeed) FileName() string {
	return sf.sysName + ".xml"
}

func (sf *showFeed) PubDate() time.Time {
	return sf.pubDate
}

func (sf *showFeed) ReadSeeker() io.ReadSeeker {
	return bytes.NewReader(sf.data)
}

func findByteRange(buf, after, before []byte) (off, end int) {
	i := bytes.Index(buf, after)
	j := bytes.Index(buf, before)
	if i != -1 && j != -1 {
		off = i + len(after)
		end = j
		return off, end
	}
	return -1, -1
}

type edit struct {
	off  int
	end  int
	text string
}

func applyEdits(buf []byte, edits []edit) []byte {
	if len(edits) == 0 {
		return buf
	}

	slices.SortFunc(edits, func(l, r edit) int {
		return cmp.Compare(l.off, r.off)
	})

	// conservatively add a bit extra room
	size := len(buf)
	for _, e := range edits {
		size += len(e.text)
	}

	newbuf := make([]byte, 0, size)
	end := 0
	for _, e := range edits {
		newbuf = append(newbuf, buf[end:e.off]...)
		newbuf = append(newbuf, e.text...)
		end = e.end
	}
	newbuf = append(newbuf, buf[end:]...)
	return newbuf
}

var (
	imageBegin      = []byte("<image>")
	imageEnd        = []byte("</image>")
	itunesNameBegin = []byte("<itunes:name>")
	itunesNameEnd   = []byte("</itunes:name>")
)

func fixShowTitle(prelude []byte, name string) (edits []edit) {
	var nameBuf bytes.Buffer
	xml.Escape(&nameBuf, []byte(name))
	name = nameBuf.String()

	// <title> not in <image>
	ioff, iend := findByteRange(prelude, imageBegin, imageEnd)
	if ioff != -1 && iend != -1 {
		off, end := findByteRange(prelude, titleBegin, titleEnd)
		// make sure <title> is not inside <image>
		if off < ioff && end < ioff || off > iend && end > iend {
			edits = append(edits, edit{
				off:  off,
				end:  end,
				text: name,
			})
		}
	}

	// <itunes:name>
	off, end := findByteRange(prelude, itunesNameBegin, itunesNameEnd)
	if off != -1 && end != -1 {
		edits = append(edits, edit{
			off:  off,
			end:  end,
			text: name,
		})
	}

	return edits
}

var (
	itunesBlockBegin = []byte("<itunes:block>")
	itunesBlockEnd   = []byte("</itunes:block>")
)

func markFeedPrivate(prelude []byte) (e edit, ok bool) {
	off, end := findByteRange(prelude, itunesBlockBegin, itunesBlockEnd)
	if off != -1 && end != -1 {
		e = edit{off: off, end: end, text: "Yes"}
		ok = true
	}
	return e, ok
}

var (
	pubDateBegin = []byte("<pubDate>")
	pubDateEnd   = []byte("</pubDate>")
)

func pubDateOrNow(prelude []byte) time.Time {
	i, j := findByteRange(prelude, pubDateBegin, pubDateEnd)

	pubDate := prelude[i:j]
	t, err := time.Parse(time.RFC1123Z, string(pubDate))
	if err != nil {
		t = time.Now()
	}

	return t
}

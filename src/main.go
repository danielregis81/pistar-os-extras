// pistar-extras — runs ON Pi-Star OS (MW0MWZ) and serves the endpoints the classic Pi-Star had,
// in the classic formats, so existing CYD panels, collectors and apps keep working.
//
//   /cyd/status.json        pistar-cyd-panel format (CYD panels, apps)
//   /cyd/flags/<cc>.565     RGB565 flags for the CYD
//   /mmdvmhost/lh.php       "Last Heard" table in the classic HTML layout
//   /images/flags/<cc>.svg  SVG flags (flagcdn.com, cached on disk)
//   /extras/health          bridge status
//   anything else, via Cloudflare Tunnel -> reverse proxy to the dashboard (https://127.0.0.1:443, WebSocket too)
//   anything else, from the LAN          -> redirect to https://<host>/ (as the dashboard does on :80)
//
// Activity is read from the dashboard's own WebSocket (wss://127.0.0.1/ws).
// Env: LISTEN (default :8081), DASHBOARD (127.0.0.1:443), DATA (/data/opt/pistar-extras).
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed countries.json
var countriesJSON []byte

var (
	listen    = env("LISTEN", ":8081")
	dashboard = env("DASHBOARD", "127.0.0.1:443")
	dataDir   = env("DATA", "/data/opt/pistar-extras")
	names     map[string]string
	ccRe      = regexp.MustCompile(`^[a-z]{2}$`)

	mu        sync.Mutex
	entries   = map[string]Entry{}
	station   string
	connected bool
	lastMsg   time.Time
)

type Entry struct {
	ID        string   `json:"id"`
	Mode      string   `json:"mode"`
	Callsign  string   `json:"callsign"`
	Target    string   `json:"target"`
	Source    string   `json:"source"`
	Slot      int      `json:"slot"`
	Status    string   `json:"status"`
	Duration  *float64 `json:"duration"`
	Loss      *float64 `json:"loss"`
	Ber       *float64 `json:"ber"`
	Rssi      *float64 `json:"rssi"`
	Timestamp string   `json:"timestamp"`
	Extra     struct {
		From  string `json:"from"`
		Name  string `json:"name"`
		SrcID int64  `json:"srcId"`
	} `json:"extra"`
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func ascii(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		if r < 128 && !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func num(f *float64) string {
	if f == nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", *f), "0"), ".")
}

// ---------------- WebSocket do painel ----------------

func wsLoop() {
	for {
		if err := wsOnce(); err != nil {
			log.Println("websocket:", err)
		}
		mu.Lock()
		connected = false
		mu.Unlock()
		time.Sleep(5 * time.Second)
	}
}

func wsOnce() error {
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", dashboard,
		&tls.Config{InsecureSkipVerify: true}) // certificado autoassinado do próprio Pi-Star
	if err != nil {
		return err
	}
	defer c.Close()
	k := make([]byte, 16)
	rand.Read(k)
	fmt.Fprintf(c, "GET /ws HTTP/1.1\r\nHost: 127.0.0.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nOrigin: https://127.0.0.1\r\n\r\n",
		base64.StdEncoding.EncodeToString(k))
	r := bufio.NewReader(c)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 101 {
		return fmt.Errorf("handshake %s", resp.Status)
	}
	mu.Lock()
	connected = true
	mu.Unlock()
	log.Println("conectado ao painel")
	var frag []byte
	for {
		c.SetReadDeadline(time.Now().Add(90 * time.Second))
		h := make([]byte, 2)
		if _, err := io.ReadFull(r, h); err != nil {
			return err
		}
		n := uint64(h[1] & 127)
		if n == 126 {
			b := make([]byte, 2)
			io.ReadFull(r, b)
			n = uint64(binary.BigEndian.Uint16(b))
		} else if n == 127 {
			b := make([]byte, 8)
			io.ReadFull(r, b)
			n = binary.BigEndian.Uint64(b)
		}
		var mask []byte
		if h[1]&128 != 0 {
			mask = make([]byte, 4)
			io.ReadFull(r, mask)
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(r, p); err != nil {
			return err
		}
		for i := range p {
			if mask != nil {
				p[i] ^= mask[i%4]
			}
		}
		switch op := h[0] & 15; op {
		case 9: // ping -> pong (mascarado: somos cliente)
			m := make([]byte, 4)
			rand.Read(m)
			out := append([]byte{0x8A, 0x80 | byte(len(p))}, m...)
			for i, b := range p {
				out = append(out, b^m[i%4])
			}
			c.Write(out)
		case 8:
			return fmt.Errorf("fechado pelo painel")
		case 0, 1:
			frag = append(frag, p...)
			if h[0]&0x80 == 0 {
				continue
			}
			handle(frag)
			frag = nil
		}
	}
}

func handle(data []byte) {
	var m struct {
		Type    string          `json:"type"`
		Config  json.RawMessage `json:"config"`
		Entries []Entry         `json:"entries"`
		Entry   *Entry          `json:"entry"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	lastMsg = time.Now()
	switch m.Type {
	case "modem_config":
		var cfg struct {
			Callsign string `json:"callsign"`
		}
		json.Unmarshal(m.Config, &cfg)
		if cfg.Callsign != "" {
			station = cfg.Callsign
		}
	case "activity_batch":
		entries = map[string]Entry{}
		for _, e := range m.Entries {
			if e.ID != "" {
				entries[e.ID] = e
			}
		}
	case "activity":
		if m.Entry != nil && m.Entry.ID != "" {
			entries[m.Entry.ID] = *m.Entry
			if len(entries) > 400 { // guarda só as mais recentes
				ks := sortedIDs()
				for _, k := range ks[300:] {
					delete(entries, k)
				}
			}
		}
	}
}

// ---------------- estado no formato antigo ----------------

type Row struct {
	Call, Name, CC, Country, Slot, TG, Src, Dur, Loss, Ber, Rssi string
	ID                                                         int64
	Start                                                      int64
	Active                                                     bool
}

func sortedIDs() []string { // mais recente primeiro (chamar com mu travado)
	ks := make([]string, 0, len(entries))
	for k := range entries {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return entries[ks[i]].Timestamp > entries[ks[j]].Timestamp })
	return ks
}

func rows() []Row {
	mu.Lock()
	defer mu.Unlock()
	var out []Row
	for _, k := range sortedIDs() {
		e := entries[k]
		if e.Mode != "DMR" && e.Mode != "" {
			continue
		}
		cc := strings.ToLower(e.Extra.From)
		// subdivisões ISO 3166-2 (ex.: gb-eng, gb-sct): usa o país (gb), que todo cliente conhece
		if i := strings.IndexByte(cc, '-'); i == 2 {
			cc = cc[:2]
		}
		if !ccRe.MatchString(cc) {
			cc = ""
		}
		t, _ := time.Parse("2006-01-02T15:04:05Z", e.Timestamp)
		r := Row{Call: e.Callsign, Name: ascii(e.Extra.Name), CC: cc, Country: ascii(names[cc]),
			TG: e.Target, Src: "Net", ID: e.Extra.SrcID, Start: t.Unix(), Active: e.Status == "in_progress"}
		if t.IsZero() {
			r.Start = 0
		}
		if e.Source == "rf" {
			r.Src = "RF"
		}
		if e.Slot > 0 {
			r.Slot = fmt.Sprintf("TS%d", e.Slot)
		}
		if !r.Active && e.Duration != nil {
			r.Dur = fmt.Sprintf("%.1f", *e.Duration)
		}
		if e.Loss != nil {
			r.Loss = num(e.Loss) + "%"
		}
		if e.Ber != nil {
			r.Ber = num(e.Ber) + "%"
		}
		r.Rssi = num(e.Rssi)
		out = append(out, r)
	}
	return out
}

func statusJSON() []byte {
	rs := rows()
	day := time.Now().Format("2006-01-02")
	calls, ccs := map[string]bool{}, map[string]bool{}
	for _, r := range rs {
		if r.Start > 0 && time.Unix(r.Start, 0).Format("2006-01-02") == day {
			calls[r.Call] = true
			if r.CC != "" {
				ccs[r.CC] = true
			}
		}
	}
	type last struct {
		Call  string `json:"call"`
		CC    string `json:"cc"`
		Start int64  `json:"start"`
	}
	out := map[string]any{"ts": time.Now().Unix(), "active": false, "cur": nil, "last": []last{},
		"today": len(calls), "countries": len(ccs)}
	mu.Lock()
	out["ok"], out["station"] = connected, station
	mu.Unlock()
	if len(rs) > 0 {
		c := rs[0]
		out["active"] = c.Active
		out["cur"] = map[string]any{"call": c.Call, "mode": "DMR", "slot": c.Slot, "tg": c.TG, "src": c.Src,
			"dur": c.Dur, "loss": c.Loss, "ber": c.Ber, "rssi": c.Rssi, "start": c.Start, "id": c.ID,
			"name": c.Name, "cc": c.CC, "country": c.Country}
		seen, ls := map[string]bool{c.Call: true}, []last{}
		for _, r := range rs[1:] {
			if seen[r.Call] {
				continue
			}
			seen[r.Call] = true
			ls = append(ls, last{r.Call, r.CC, r.Start})
			if len(ls) >= 6 {
				break
			}
		}
		out["last"] = ls
	}
	b, _ := json.Marshal(out)
	return b
}

func lhHTML() string {
	var b strings.Builder
	b.WriteString("<table>\n")
	rs := rows()
	if len(rs) > 20 {
		rs = rs[:20]
	}
	for _, r := range rs {
		t := time.Unix(r.Start, 0)
		d := t.Day()
		suf := "th"
		if d%100 < 10 || d%100 > 20 {
			suf = map[int]string{1: "st", 2: "nd", 3: "rd"}[d%10]
			if suf == "" {
				suf = "th"
			}
		}
		cs := html.EscapeString(r.Call)
		cell := fmt.Sprintf(`<a href="https://www.qrz.com/db/%s" target="_blank">%s</a> <span class="lh-opname">%s</span>`,
			cs, cs, html.EscapeString(r.Name))
		if r.CC != "" {
			cell += fmt.Sprintf(` <img src="images/flags/%s.svg"><span class="lh-tip">%s</span>`, r.CC, html.EscapeString(r.Country))
		}
		dur := r.Dur
		if r.Active {
			dur = "TX"
		} else if dur == "" {
			dur = "0"
		}
		fmt.Fprintf(&b, "<tr><td>%s %s %d%s</td><td>DMR %s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			t.Format("15:04:05"), t.Format("Jan"), d, suf, r.Slot, cell, html.EscapeString(r.TG), r.Src, dur, r.Loss, r.Ber)
	}
	b.WriteString("</table>\n")
	return b.String()
}

func flagSVG(cc string) ([]byte, error) {
	p := filepath.Join(dataDir, "flags-svg", cc+".svg")
	if b, err := os.ReadFile(p); err == nil {
		return b, nil
	}
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Get("https://flagcdn.com/" + cc + ".svg")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.HasPrefix(strings.TrimSpace(string(b)), "<") {
		return nil, fmt.Errorf("flagcdn %d", resp.StatusCode)
	}
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p+".tmp", b, 0644)
	os.Rename(p+".tmp", p)
	return b, nil
}

// ---------------- HTTP ----------------

var (
	reSVG = regexp.MustCompile(`^/images/flags/([a-z]{2})\.svg$`)
	re565 = regexp.MustCompile(`^/cyd/flags/([a-z]{2})\.565$`)
)

func main() {
	var c struct {
		Names map[string]string `json:"names"`
	}
	if err := json.Unmarshal(countriesJSON, &c); err != nil {
		log.Fatal(err)
	}
	names = c.Names
	go wsLoop()

	target, _ := url.Parse("https://" + dashboard)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	proxy.FlushInterval = -1
	orig := proxy.Director
	proxy.Director = func(r *http.Request) {
		host := r.Host
		orig(r)
		r.Host = host // o painel vê o endereço público (redirects e cookies certos)
		r.Header.Set("X-Forwarded-Proto", "https")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		nocache := func(ct string) {
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", "no-store")
		}
		switch {
		case p == "/cyd/status.json":
			nocache("application/json")
			w.Write(statusJSON())
		case p == "/mmdvmhost/lh.php":
			nocache("text/html; charset=utf-8")
			io.WriteString(w, lhHTML())
		case p == "/extras/health":
			mu.Lock()
			b, _ := json.Marshal(map[string]any{"connected": connected, "entries": len(entries),
				"last_msg_age": int(time.Since(lastMsg).Seconds())})
			mu.Unlock()
			nocache("application/json")
			w.Write(b)
		case reSVG.MatchString(p):
			b, err := flagSVG(reSVG.FindStringSubmatch(p)[1])
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "max-age=604800")
			w.Write(b)
		case re565.MatchString(p):
			http.ServeFile(w, r, filepath.Join(dataDir, "flags565", re565.FindStringSubmatch(p)[1]+".565"))
		case r.Header.Get("Cf-Connecting-Ip") != "":
			proxy.ServeHTTP(w, r) // veio pelo túnel (já é https lá fora)
		default:
			http.Redirect(w, r, "https://"+strings.Split(r.Host, ":")[0]+r.URL.RequestURI(), http.StatusFound)
		}
	})
	log.Println("ouvindo em", listen)
	log.Fatal(http.ListenAndServe(listen, nil))
}

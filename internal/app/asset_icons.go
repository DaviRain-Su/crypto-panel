package app

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"image"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/intents"
)

const (
	maxAssetIconBytes = 2 << 20
	// maxIconEdge keeps a token icon far below a decode bomb. 512×512 RGBA
	// is 1 MiB, inside the 2 MiB compressed-byte budget.
	maxIconEdge         = 512
	maxCachedIcons      = 32
	iconFailureCooldown = 30 * time.Second
)

//go:embed assetart/*.png assetart/*.svg
var builtinAssetArt embed.FS

var builtinAssetIcons = map[string]string{
	"btc": "assetart/btc.png", "eth": "assetart/eth.png", "weth": "assetart/eth.png",
	"usdc": "assetart/usdc.png", "usdt": "assetart/usdt.png", "dai": "assetart/dai.png",
	"bnb": "assetart/bnb.png", "avax": "assetart/avax.png", "sol": "assetart/sol.png",
}

// builtinAssetIcon is synchronous and local, so common assets never wait on
// a third-party image host. NEAR's mark comes from near.com's network icon.
func builtinAssetIcon(token intents.Token) ui.ImageSource {
	symbol := strings.ToLower(token.Symbol)
	if token.Blockchain == "near" && (symbol == "near" || symbol == "wnear") {
		data, err := builtinAssetArt.ReadFile("assetart/near.svg")
		if err == nil {
			if svg, err := ui.ParseSVG(data); err == nil {
				return svg
			}
		}
	}
	path := builtinAssetIcons[strings.ToLower(token.Symbol)]
	if path == "" {
		return nil
	}
	return cachedBuiltinIcon(path)
}

var (
	builtinIconMu sync.Mutex
	builtinIcons  = map[string]ui.ImageSource{}
)

func cachedBuiltinIcon(path string) ui.ImageSource {
	builtinIconMu.Lock()
	if icon, ok := builtinIcons[path]; ok {
		builtinIconMu.Unlock()
		return icon
	}
	builtinIconMu.Unlock()
	data, err := builtinAssetArt.ReadFile(path)
	if err != nil {
		return nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	builtinIconMu.Lock()
	builtinIcons[path] = bitmap
	builtinIconMu.Unlock()
	return bitmap
}

type assetIconResult struct {
	bitmap *ui.Bitmap
	err    error
}

type iconFlight struct {
	done   chan struct{}
	result assetIconResult
}

// assetIconCache keeps public asset art out of the quote request path.
type assetIconCache struct {
	http      *http.Client
	allowHTTP string
	mu        sync.Mutex
	entries   map[string]assetIconResult
	inflight  map[string]*iconFlight
	failed    map[string]time.Time
}

func newAssetIconCache(client *http.Client) *assetIconCache {
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	icons := &assetIconCache{http: &clone, entries: map[string]assetIconResult{}}
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many icon redirects")
		}
		if len(via) > 0 && !strings.EqualFold(via[0].URL.Hostname(), req.URL.Hostname()) {
			return errors.New("asset icon redirected to another host")
		}
		return icons.validateURL(req.URL.String())
	}
	return icons
}

func (c *assetIconCache) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid asset icon URL")
	}
	if u.User != nil || u.Host == "" || (u.Scheme != "https" && !(c.allowHTTP != "" && strings.HasPrefix(raw, c.allowHTTP+"/"))) {
		return errors.New("asset icon URL must be public HTTPS")
	}
	testServer := c.allowHTTP != "" && strings.HasPrefix(raw, c.allowHTTP+"/")
	if !testServer && privateIconHost(u.Hostname()) {
		return errors.New("asset icon URL must be public HTTPS")
	}
	return nil
}

func privateIconHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimRight(host, ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := parseIconIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || sharedIconAddress(ip)
}

// parseIconIP accepts the numeric spellings resolvers treat as addresses:
// a short dotted form such as 127.1, a decimal integer, and a hex integer.
func parseIconIP(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	if strings.Contains(host, ".") {
		return parseInetAton(host)
	}
	if host == "" {
		return nil
	}
	for _, r := range host {
		if r != 'x' && (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return nil
		}
	}
	return ipv4FromNumber(host)
}

func parseInetAton(host string) net.IP {
	parts := strings.Split(host, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil
	}
	nums := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil
		}
		n, err := strconv.ParseUint(part, 0, 64)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	var ip uint64
	switch len(nums) {
	case 2:
		if nums[0] > 0xff || nums[1] > 0xffffff {
			return nil
		}
		ip = nums[0]<<24 | nums[1]
	case 3:
		if nums[0] > 0xff || nums[1] > 0xff || nums[2] > 0xffff {
			return nil
		}
		ip = nums[0]<<24 | nums[1]<<16 | nums[2]
	default:
		for _, n := range nums {
			if n > 0xff {
				return nil
			}
		}
		ip = nums[0]<<24 | nums[1]<<16 | nums[2]<<8 | nums[3]
	}
	return net.IPv4(byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip)).To4()
}

func ipv4FromNumber(host string) net.IP {
	n, err := strconv.ParseUint(host, 0, 64)
	if err != nil || n > 0xffffffff {
		return nil
	}
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).To4()
}

func sharedIconAddress(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 0x40
}

func (c *assetIconCache) cached(raw string) (assetIconResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.entries[raw]
	return result, ok
}

func (c *assetIconCache) loading(raw string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.inflight[raw]
	return ok
}

// cooling reports a recent failure. The error is not stored as an icon, and
// the frame loop waits before trying the URL again.
func (c *assetIconCache) cooling(raw string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.failed[raw]
	if !ok {
		return false
	}
	if time.Since(at) >= iconFailureCooldown {
		delete(c.failed, raw)
		return false
	}
	return true
}

func (c *assetIconCache) noteFailure(raw string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed == nil {
		c.failed = map[string]time.Time{}
	}
	c.failed[raw] = time.Now()
}

func (c *assetIconCache) load(ctx context.Context, raw string) (*ui.Bitmap, error) {
	c.mu.Lock()
	if result, ok := c.entries[raw]; ok {
		c.mu.Unlock()
		return result.bitmap, result.err
	}
	if flight, ok := c.inflight[raw]; ok {
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.result.bitmap, flight.result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &iconFlight{done: make(chan struct{})}
	if c.inflight == nil {
		c.inflight = map[string]*iconFlight{}
	}
	c.inflight[raw] = flight
	c.mu.Unlock()

	result := c.fetchIcon(ctx, raw)
	c.mu.Lock()
	delete(c.inflight, raw)
	if result.err == nil && result.bitmap != nil {
		if len(c.entries) >= maxCachedIcons {
			for key := range c.entries {
				delete(c.entries, key)
				break
			}
		}
		c.entries[raw] = result
	}
	flight.result = result
	close(flight.done)
	c.mu.Unlock()
	return result.bitmap, result.err
}

func (c *assetIconCache) fetchIcon(ctx context.Context, raw string) assetIconResult {
	var result assetIconResult
	result.err = c.validateURL(raw)
	if result.err != nil {
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		result.err = err
		return result
	}
	res, err := c.http.Do(req)
	if err != nil {
		result.err = err
		return result
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		result.err = errors.New("asset icon request failed")
		return result
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAssetIconBytes+1))
	if err != nil {
		result.err = err
		return result
	}
	if len(data) > maxAssetIconBytes {
		result.err = errors.New("asset icon is too large")
		return result
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		result.err = err
		return result
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxIconEdge || cfg.Height > maxIconEdge || pixels > int64(maxAssetIconBytes)/4 {
		result.err = errors.New("asset icon dimensions are too large")
		return result
	}
	result.bitmap, result.err = ui.DecodeBitmap(data)
	return result
}

func assetMonogram(symbol string) string {
	for _, r := range strings.TrimSpace(symbol) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

// assetIcon draws only a completed bitmap and starts one background load.
// A failure stays a monogram and is not cached, so a later load can succeed.
func (a *App) assetIcon(c *ui.Context, token intents.Token, size float32) {
	if icon := builtinAssetIcon(token); icon != nil {
		ui.Box(c).Size(size, size).Radius(size / 2).Clip().Children(func() {
			ui.Image(c, icon).Size(size, size)
		})
		return
	}
	if token.IconURL != "" && a.assetIcons != nil {
		if result, ok := a.assetIcons.cached(token.IconURL); ok {
			if result.bitmap != nil {
				ui.Box(c).Size(size, size).Radius(size / 2).Clip().Children(func() {
					ui.Image(c, result.bitmap).Size(size, size)
				})
				return
			}
		} else if !a.assetIcons.loading(token.IconURL) && !a.assetIcons.cooling(token.IconURL) {
			raw := token.IconURL
			a.goAsync(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if _, err := a.assetIcons.load(ctx, raw); err != nil {
					a.assetIcons.noteFailure(raw)
				}
				a.runOnMain(a.invalidate)
			})
		}
	}
	ui.Box(c).Size(size, size).Radius(size / 2).Background(a.pal.accentSoft).Center().Children(func() {
		ui.Text(c, assetMonogram(token.Symbol)).FontSize(size * 0.42).FontWeight(700).TextColor(a.pal.accent)
	})
}

func (a *App) assetIdentity(c *ui.Context, token intents.Token, amount string) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		a.assetIcon(c, token, 32)
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, amount).FontSize(13).FontWeight(650).TextColor(a.pal.text).MaxLines(2)
			ui.Text(c, blockchainName(token.Blockchain)).FontSize(10.5).TextColor(a.pal.muted).MaxLines(1)
		})
	})
}

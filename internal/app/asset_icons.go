package app

import (
	"context"
	"embed"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/intents"
)

const maxAssetIconBytes = 2 << 20

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
	data, err := builtinAssetArt.ReadFile(path)
	if err != nil {
		return nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bitmap
}

type assetIconResult struct {
	bitmap *ui.Bitmap
	err    error
}

// assetIconCache keeps public asset art out of the quote request path.
type assetIconCache struct {
	http      *http.Client
	allowHTTP string
	mu        sync.Mutex
	entries   map[string]assetIconResult
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
	return nil
}

func (c *assetIconCache) cached(raw string) (assetIconResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.entries[raw]
	return result, ok
}

func (c *assetIconCache) load(ctx context.Context, raw string) (*ui.Bitmap, error) {
	if result, ok := c.cached(raw); ok {
		return result.bitmap, result.err
	}
	result := assetIconResult{err: c.validateURL(raw)}
	if result.err == nil {
		var req *http.Request
		req, result.err = http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if result.err == nil {
			var res *http.Response
			res, result.err = c.http.Do(req)
			if result.err == nil {
				defer res.Body.Close()
				if res.StatusCode != http.StatusOK {
					result.err = errors.New("asset icon request failed")
				} else {
					var data []byte
					data, result.err = io.ReadAll(io.LimitReader(res.Body, maxAssetIconBytes+1))
					if result.err == nil && len(data) > maxAssetIconBytes {
						result.err = errors.New("asset icon is too large")
					}
					if result.err == nil {
						result.bitmap, result.err = ui.DecodeBitmap(data)
					}
				}
			}
		}
	}
	c.mu.Lock()
	c.entries[raw] = result
	c.mu.Unlock()
	return result.bitmap, result.err
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
// Failures remain a monogram; a failed URL is not retried on every frame.
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
		} else {
			raw := token.IconURL
			a.goAsync(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				_, _ = a.assetIcons.load(ctx, raw)
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

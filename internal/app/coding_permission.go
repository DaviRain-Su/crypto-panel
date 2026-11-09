package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/i18n"
)

// codingPermissionWindow 是编码许可提示。测试用替身触发关闭，生产路径打开原生窗口。
type codingPermissionWindow interface {
	OnClose(func(*mygo.CloseEvent)) func()
	Focus()
	Close()
}

var openCodingPermissionWindow = func(opts mygo.WindowOptions) codingPermissionWindow {
	return mygo.NewWindow(opts)
}

func codingOption(request acp.PermissionRequest, id string) string {
	for _, option := range request.Options {
		if option.OptionID == id {
			return id
		}
	}
	return ""
}

// 编码许可只回答对端列出的 optionId；窗口关闭、取消和异常一律拒绝。
func (a *App) requestCodingPermission(ctx context.Context, request acp.PermissionRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(request.Options) > 16 || len(request.ToolCall.Raw) > 16<<10 {
		return "", nil
	}
	answer := make(chan string, 1)
	var dismiss func()
	a.runOnMain(func() {
		if a.win == nil || a.quitting || ctx.Err() != nil {
			answer <- ""
			return
		}
		var win codingPermissionWindow
		decided := false
		decide := func(id string) {
			finishCodingPermission(&decided, answer, win, request, id)
		}
		dismiss = func() { decide("") }
		var formatted bytes.Buffer
		raw := request.ToolCall.Raw
		if json.Indent(&formatted, raw, "", "  ") == nil {
			raw = formatted.Bytes()
		}
		detail := safeAgentText(string(raw), 20<<10)
		win = openCodingPermissionWindow(mygo.WindowOptions{
			Title: a.t(i18n.CodingWindowTitle), Width: 620, Height: 620, MinWidth: 460, MinHeight: 500, Parent: a.win, Modal: true, BackgroundColor: "light-dark(#f6f4ef, #141513)",
			Content: ui.View(codingPermissionView(a, ctx, request, detail, decide)),
		})
		win.OnClose(func(*mygo.CloseEvent) { decide("") })
		win.Focus()
	})
	select {
	case id := <-answer:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return id, nil
	case <-ctx.Done():
		a.runOnMain(func() {
			if dismiss != nil {
				dismiss()
			}
		})
		return "", ctx.Err()
	}
}

func finishCodingPermission(decided *bool, answer chan<- string, win codingPermissionWindow, request acp.PermissionRequest, id string) {
	if *decided {
		return
	}
	*decided = true
	answer <- codingOption(request, id)
	if win != nil {
		win.Close()
	}
}

func codingPermissionView(a *App, ctx context.Context, request acp.PermissionRequest, detail string, decide func(string)) func(c *ui.Context) {
	return func(c *ui.Context) {
		t := c.Theme()
		p := paletteFor(t)
		useAccent(t, p)
		c.Root().Background(p.bg)
		ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
			ui.Text(c, a.t(i18n.CodingPermissionTitle)).FontSize(20).FontWeight(700)
			ui.Text(c, safeAgentText(request.ToolCall.Title, 200)).FontSize(14).FontWeight(600).MaxLines(4)
			ui.Text(c, a.t(i18n.CodingPermissionBody)).FontSize(12).TextColor(p.muted).MaxLines(4)
			ui.Text(c, a.t(i18n.CodingPermissionNotWallet)).FontSize(12).TextColor(p.danger)
			ui.TextArea(c.Key("coding-permission-detail"), &detail).ReadOnly(true).Grow(1).Font("monospace").FontSize(10.5)
			ui.Row(c).Gap(8).Wrap().Justify(ui.End).Children(func() {
				ui.Button(c, a.t(i18n.CodingDeny)).OnClick(func() { decide("") })
				for _, option := range request.Options {
					option := option
					label := safeAgentText(option.Name, 80)
					click := func() {
						if ctx.Err() != nil {
							decide("")
						} else {
							decide(option.OptionID)
						}
					}
					if strings.Contains(strings.ToLower(option.Kind), "allow") {
						ui.PrimaryButton(c, label).OnClick(click)
						continue
					}
					ui.Button(c, label).OnClick(click)
				}
			})
		})
	}
}

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestDappSchemeHTTPRoute(t *testing.T) {
	a := newTestApp(t)
	for _, tc := range []struct {
		url    string
		method string
		want   int
	}{
		{dappRequestURL, "POST", http.StatusForbidden}, // Reaches source validation, not 404.
		{"cryptopanel://near/near/request", "POST", http.StatusNotFound},
		{"cryptopanel:///near/request", "POST", http.StatusNotFound},
		{"cryptopanel://attacker/request", "POST", http.StatusNotFound},
		{"cryptopanel://near/request?x=1", "POST", http.StatusNotFound},
		{dappRequestURL, "GET", http.StatusNotFound},
	} {
		r := httptest.NewRequest(tc.method, tc.url, strings.NewReader(`{"id":"1","method":"getNetwork"}`))
		w := httptest.NewRecorder()
		a.serveDappRequest(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s: status %d, want %d", tc.method, tc.url, w.Code, tc.want)
		}
	}
}

func dappHTTP(t *testing.T, a *App, bridge *dappBridgeState, page *mygo.Page, origin string, ctx context.Context, req dappRequest) (*httptest.ResponseRecorder, []dappResponse) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, dappRequestURL, bytes.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	var replies []dappResponse
	a.serveDappRequestFrom(w, r, bridge, page, origin, func(out dappResponse) { replies = append(replies, out) })
	return w, replies
}

func TestDappHTTPKeepsConfirmedBatchOutcome(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	origin := "https://dapp.test"
	a.dappConnected[origin] = true
	n := 0
	a.approve = func(approvalRequest) bool {
		n++
		return n == 1
	}
	b := a.dappBridge
	b.committed(origin)
	binding, _ := b.binding()
	w, replies := dappHTTP(t, a, b, binding.page, origin, t.Context(), dappRequest{
		ID: "batch", Method: "signAndSendTransactions", Capability: binding.capability,
		Params: json.RawMessage(`{"transactions":[
			{"receiverId":"one.testnet","actions":[{"type":"transfer","amount":"1"}]},
			{"receiverId":"two.testnet","actions":[{"type":"transfer","amount":"2"}]}
		]}`),
	})
	if w.Code != http.StatusNoContent || len(replies) != 1 {
		t.Fatalf("status=%d replies=%d", w.Code, len(replies))
	}
	if len(rpc.broadcasts) != 1 {
		t.Fatalf("broadcasts = %d, want the confirmed transaction only", len(rpc.broadcasts))
	}
	raw, err := json.Marshal(replies[0])
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Error  string            `json:"error"`
		Result []json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &body) != nil {
		t.Fatalf("response = %s", raw)
	}
	if !strings.Contains(body.Error, "transaction 2") || !strings.Contains(body.Error, "rejected") || len(body.Result) != 1 {
		t.Fatalf("response dropped the confirmed outcome: %s", raw)
	}
	var outcome map[string]any
	if json.Unmarshal(body.Result[0], &outcome) != nil || outcome["hash"] == nil || outcome["transaction"] == nil {
		t.Fatalf("confirmed outcome = %s", body.Result[0])
	}
}

func TestDappHTTPRequiresDocumentCapabilityEvenWithConnectedTopPage(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	origin := "https://dapp.test"
	a.dappConnected[origin] = true
	b := a.dappBridge
	b.committed(origin)
	binding, _ := b.binding()
	params := json.RawMessage(`{"receiverId":"bob.testnet","actions":[{"type":"transfer","amount":"1"}]}`)
	for _, capability := range []string{"", strings.Repeat("f", 64)} {
		// frame 可以发送 POST 并伪造此 header，但二者均不能取得权限。
		body, _ := json.Marshal(dappRequest{ID: "frame", Method: "signAndSendTransaction", Params: params, Capability: capability})
		r := httptest.NewRequest("POST", dappRequestURL, bytes.NewReader(body))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		a.serveDappRequestFrom(w, r, b, binding.page, origin, func(dappResponse) { t.Error("an unproven frame received a reply") })
		if w.Code != http.StatusForbidden {
			t.Fatalf("unproven frame status = %d", w.Code)
		}
	}
	if len(rpc.calls) != 0 || rpc.broadcast != nil {
		t.Fatal("a frame without the main document's capability reached signing RPC")
	}
	// 即使 WK 不提供 Origin，合法主 frame 的 capability 仍可使用。
	w, replies := dappHTTP(t, a, b, binding.page, origin, t.Context(), dappRequest{
		ID: "main", Method: "signAndSendTransaction", Params: params, Capability: binding.capability,
	})
	if w.Code != http.StatusNoContent || len(replies) != 1 || replies[0].Error != "" || replies[0].Origin != origin || rpc.broadcast == nil {
		t.Fatalf("main-frame status=%d replies=%+v broadcast=%v", w.Code, replies, rpc.broadcast != nil)
	}
}

func TestDappHTTPRejectsOldDocumentOriginAndOtherPage(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	b := a.dappBridge
	origin := "https://dapp.test"
	b.committed(origin)
	old, _ := b.binding()
	b.committed(origin) // A same-URL reload is a new document too.
	current, _ := b.binding()
	if old.capability == current.capability || b.current(old) {
		t.Fatal("a document reload retained authority")
	}
	for _, tc := range []struct {
		page       *mygo.Page
		origin     string
		capability string
	}{
		{old.page, origin, old.capability},
		{new(mygo.Page), origin, current.capability},
		{current.page, "https://other.test", current.capability},
	} {
		w, replies := dappHTTP(t, a, b, tc.page, tc.origin, t.Context(), dappRequest{
			ID: "replay", Method: "getNetwork", Capability: tc.capability,
		})
		if w.Code != http.StatusForbidden || len(replies) != 0 {
			t.Fatalf("replay status=%d replies=%+v", w.Code, replies)
		}
	}
	b.committed("https://other.test")
	w, _ := dappHTTP(t, a, b, old.page, "https://other.test", t.Context(), dappRequest{
		ID: "old-origin", Method: "signAndSendTransaction", Capability: old.capability,
	})
	if w.Code != http.StatusForbidden || len(rpc.calls) != 0 {
		t.Fatal("the old origin's token became authority at a new origin")
	}
}

func TestDappHTTPRejectsClaimedOriginAndTrailingJSON(t *testing.T) {
	a, _, _ := newDappApp(t)
	b := a.dappBridge
	b.committed("https://dapp.test")
	binding, _ := b.binding()
	for _, body := range []string{
		`{"id":"x","method":"connect","origin":"https://dapp.test"}`,
		`{"id":"x","method":"connect"}{"id":"second"}`,
		strings.Repeat("x", (64<<10)+1),
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", dappRequestURL, strings.NewReader(body))
		a.serveDappRequestFrom(w, r, b, binding.page, binding.origin, func(dappResponse) { t.Error("invalid request replied") })
		if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("invalid body status = %d", w.Code)
		}
	}
}

func TestDappHTTPPendingSigningCanceled(t *testing.T) {
	for _, reason := range []string{"fetch abort", "timeout", "navigation", "close", "disconnect"} {
		t.Run(reason, func(t *testing.T) {
			a, rpc, _ := newDappApp(t)
			b := a.dappBridge
			origin := "https://dapp.test"
			a.dappConnected[origin] = true
			b.committed(origin)
			binding, _ := b.binding()
			started := make(chan struct{})
			b.approve = func(ctx context.Context, _ approvalRequest) bool {
				close(started)
				<-ctx.Done()
				return true // A late approval still must not read a key or broadcast.
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if reason == "timeout" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 200*time.Millisecond)
				defer deadlineCancel()
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				w, replies := dappHTTP(t, a, b, binding.page, origin, ctx, dappRequest{
					ID: "pending", Method: "signAndSendTransaction", Capability: binding.capability,
					Params: json.RawMessage(`{"receiverId":"bob.testnet","actions":[{"type":"transfer","amount":"1"}]}`),
				})
				if w.Code != http.StatusNoContent || len(replies) != 0 {
					t.Errorf("canceled request status=%d replies=%+v", w.Code, replies)
				}
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("approval never started")
			}
			switch reason {
			case "fetch abort":
				cancel()
			case "navigation":
				b.committed(origin)
			case "close":
				a.closeDapp()
			case "disconnect":
				if _, err := a.handleDappRequest(t.Context(), origin, dappRequest{ID: "disconnect", Method: "disconnect"}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("an abandoned approval remained pending")
			}
			if rpc.broadcast != nil {
				t.Fatal("a canceled request broadcast a transaction")
			}
			b.mu.Lock()
			pending := len(b.pending)
			b.mu.Unlock()
			if pending != 0 {
				t.Fatal("canceled request stayed in the pending map")
			}
		})
	}
}

func TestDappCanceledConnectCannotGrantConnection(t *testing.T) {
	a, _, _ := newDappApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	a.dappBridge.connect = func(context.Context, string) bool { cancel(); return true }
	if _, err := a.dappConnect(ctx, "https://dapp.test", nil); err == nil {
		t.Fatal("a canceled connect succeeded")
	}
	if a.dappConnected["https://dapp.test"] {
		t.Fatal("a canceled native question connected an origin")
	}
}

func TestDappDecisionCancellationReleasesApprovalQueue(t *testing.T) {
	a := newTestApp(t)
	a.win = new(mygo.Window) // dappDecision's injected open never calls native UI.
	ctx, cancel := context.WithCancel(t.Context())
	opened, closed := make(chan struct{}), make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- a.dappDecision(ctx, func(func(bool)) func() {
			close(opened)
			return func() { close(closed) }
		})
	}()
	<-opened
	cancel()
	if <-done {
		t.Fatal("a canceled question was approved")
	}
	<-closed
	if !a.approvalMu.TryLock() {
		t.Fatal("a canceled prompt retained the shared approval lock")
	}
	// 等待插件批准锁时取消请求，不能继续排队打开批准窗。
	waiting, stop := context.WithCancel(t.Context())
	queued := make(chan bool, 1)
	go func() {
		queued <- a.dappDecision(waiting, func(func(bool)) func() {
			t.Error("a canceled waiter opened a prompt")
			return nil
		})
	}()
	stop()
	select {
	case ok := <-queued:
		if ok {
			t.Fatal("a canceled queue waiter succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("a canceled request remained queued behind another approval")
	}
	a.approvalMu.Unlock()
}

func TestDappOldCompletionCannotRemoveNewRequest(t *testing.T) {
	page := new(mygo.Page)
	b := newDappBridge(page)
	b.committed("https://dapp.test")
	oldBinding, _ := b.binding()
	old, err := b.begin(t.Context(), page, oldBinding.origin, dappRequest{ID: "same", Capability: oldBinding.capability})
	if err != nil {
		t.Fatal(err)
	}
	b.committed(oldBinding.origin)
	binding, _ := b.binding()
	current, err := b.begin(t.Context(), page, binding.origin, dappRequest{ID: "same", Capability: binding.capability})
	if err != nil {
		t.Fatal(err)
	}
	b.finish(old)
	if b.pending["same"] != current || old.ctx.Err() == nil || current.ctx.Err() != nil {
		t.Fatal("an old document's completion affected a new document's request")
	}
	b.finish(current)
}

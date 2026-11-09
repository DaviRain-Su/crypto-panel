package app

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/neartx"
)

func TestApprovalLongCallsRemainReadableAndButtonsVisible(t *testing.T) {
	args := neartx.DecodeArgs([]byte(`{"memo":"` + strings.Repeat("a", 600) + `","receiver_id":"recipient.near","amount":"42"}`))
	for _, height := range []int{480, 560} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			sum := &neartx.Summary{
				SignerID: "alice.testnet", ReceiverID: "token.testnet",
				KeyPermission: &neartx.KeyPermissionSummary{FullAccess: true},
			}
			for i := 0; i < 8; i++ {
				sum.Actions = append(sum.Actions, neartx.ActionSummary{
					Kind: neartx.ActionFunctionCall, MethodName: "ft_transfer",
					ArgsText: args, Amount: big.NewInt(1), Gas: 30_000_000_000_000,
				})
			}
			ap := nearApproval(sum)
			approved := false
			ap.onDecide = func(ok bool) { approved = ok }
			tt := ui.NewTester(ap.view, 460, height)
			if !tt.HasText(args) || !strings.Contains(args, `"receiver_id":"recipient.near"`) {
				t.Fatal("批准内容丢失了长 memo 后的实际收款人或金额")
			}
			assertButtons := func() {
				t.Helper()
				for _, label := range []string{"Approve and sign", "Reject"} {
					info, ok := tt.Access(label)
					if !ok {
						t.Fatalf("找不到 %q", label)
					}
					b := info.Bounds
					if b.Y < 0 || b.Y+b.H > float32(height) || b.X < 0 || b.X+b.W > 460 {
						t.Fatalf("%q 超出窗口：%+v", label, b)
					}
				}
			}
			assertButtons()
			tt.Scroll(230, 200, 0, 10000)
			assertButtons()
			if ap.decided {
				t.Fatal("滚动详情不应触发批准")
			}
			if err := tt.Click("Approve and sign"); err != nil {
				t.Fatal(err)
			}
			if !approved {
				t.Fatal("可见的批准按钮没有响应点击")
			}
		})
	}
}

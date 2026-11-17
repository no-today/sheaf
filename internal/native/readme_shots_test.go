package native

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestReadmeShots(t *testing.T) {
	root := os.Getenv("SHEAF_README_WS")
	if root == "" {
		t.Skip("set SHEAF_README_WS to scripts/mkfixture.sh's disposable workspace")
	}
	if os.Getenv("SHEAF_SHOTS") == "" {
		t.Skip("set SHEAF_SHOTS to the screenshot output directory")
	}
	testutil.Isolate(t)
	for _, lang := range []struct{ locale, suffix string }{{"zh-Hans", "zh-CN"}, {"en-US", "en"}} {
		t.Run(lang.locale, func(t *testing.T) {
			h := viewtest.Open(t, root)
			if err := h.S.SetMergeFlow([]session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master", Branch: "master"}}); err != nil {
				t.Fatal(err)
			}
			viewtest.TrackAll(t, h.S)
			w := New(h.Update, &h.Wins)
			w.smoke = smoke{}
			t.Cleanup(w.Close)
			w.Attach(h.S)
			w.Go(view.Track, view.Smoke{Q: "PAY-142", Row: -1})
			tt := ui.NewTester(w.View, 1440, 900)
			tt.SetLocale(lang.locale)
			hr := &viewtest.Harness{T: t, H: h, TT: tt}
			hr.Until("finished workspace tasks", func() bool {
				for _, task := range h.S.Tasks() {
					if task.State == session.TaskRunning {
						return false
					}
				}
				return true
			})
			w.Tasks()
			hr.Until("the followed requirement, all six repos and the first diff", func() bool {
				for _, repo := range []string{"payment-api", "checkout-web", "ledger-service", "risk-engine", "receipt-worker", "gateway"} {
					if !tt.HasText(repo) {
						return false
					}
				}
				for _, text := range tt.Texts() {
					for _, loading := range []string{"加载中…", "正在读取…", "Loading…"} {
						if strings.Contains(text, loading) {
							return false
						}
					}
				}
				return tt.HasText("PAY-142") && tt.HasText("payment-api/internal/payments/capture.go") && tt.HasText("@@ -1,5 +1,8 @@") && !tt.FramePending()
			})
			hr.Shot("requirement-" + lang.suffix)
		})
	}
}

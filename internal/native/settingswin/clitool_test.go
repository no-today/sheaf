package settingswin

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestCLIToolHiddenWhenUnavailable(t *testing.T) {
	_, h, tt := open(t, "general", false, Deps{CLITool: func() CLITool { return CLITool{} }})
	until(t, h, tt, "general page", func() bool { return tt.HasText("关于") })
	if tt.HasText("命令行工具") || tt.HasText("安装") {
		t.Fatalf("unavailable tool shown: %q", tt.Texts())
	}
}

func TestCLIToolChanges(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		installed, cancel bool
		err               error
		message           string
	}{
		{name: "install"},
		{name: "uninstall", installed: true},
		{name: "cancel-install", cancel: true},
		{name: "cancel-uninstall", installed: true, cancel: true},
		{name: "failure", err: errors.New("permission denied"), message: "无法安装或卸载命令行工具：permission denied"},
		{name: "foreign", err: &os.PathError{Op: "change command line tool", Path: "/fixture/_sheaf", Err: ErrForeignCLITool}, message: "/fixture/_sheaf 已被其他文件或链接占用，请先手动移除。"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state := CLITool{Available: true, Installed: scenario.installed, Path: "/fixture/bin/sheaf"}
			type request struct {
				install bool
				done    func(error)
			}
			requests := make(chan request, 2)
			w, h, tt := open(t, "general", false, Deps{
				CLITool:    func() CLITool { return state },
				SetCLITool: func(install bool, done func(error)) { requests <- request{install, done} },
			})
			label, hint := "安装", "未安装"
			if scenario.installed {
				label, hint = "卸载", "已安装到 /fixture/bin/sheaf"
			}
			until(t, h, tt, "command line tool", func() bool { return tt.HasText(label) && tt.HasText(hint) })
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			var req request
			select {
			case req = <-requests:
			case <-time.After(time.Second):
				t.Fatal("setter did not run in background")
			}
			if req.install == scenario.installed || !w.cliBusy {
				t.Fatalf("request=%+v, busy=%v", req, w.cliBusy)
			}
			_ = tt.Click(label)
			tt.Frame()
			select {
			case <-requests:
				t.Fatal("busy button dispatched another change")
			default:
			}
			if !scenario.cancel && scenario.err == nil {
				state.Installed = !state.Installed
			}
			req.done(scenario.err)
			until(t, h, tt, "completion", func() bool { return !w.cliBusy })
			if scenario.message != "" && !tt.HasText(scenario.message) {
				t.Fatalf("missing failure %q: %q", scenario.message, tt.Texts())
			}
			want := map[bool]string{true: "卸载", false: "安装"}[state.Installed]
			if !tt.HasText(want) || state.Installed && !tt.HasText("已安装到 /fixture/bin/sheaf") {
				t.Fatalf("row did not follow the state: %q", tt.Texts())
			}
		})
	}
}

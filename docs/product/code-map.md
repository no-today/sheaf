# 代码地图

给想不靠 AI 读懂和修改代码的人。这里只告诉你从哪开始读，逻辑以代码为准；读不懂的地方是代码该改，记进 `ISSUES.md`。

## 跑起来

```sh
go mod download
go tool mygo dev                                    # Go 改动后自动重编译并重启
SHEAF_WORKSPACE=<目录> go tool mygo dev            # 指定工作区，多个用 : 分隔
go vet ./... && go test ./...                       # 提交前
go test -run TestDecide ./internal/gitops           # 只跑一个测试，改规则时最快的循环
```

想试写操作，别用真仓库：`scripts/mkfixture.sh /tmp/fx` 造一个一次性工作区，再 `SHEAF_HOME=$(mktemp -d) SHEAF_WORKSPACE=/tmp/fx/ws go tool mygo dev`。截图和场景变量见 `docs/agent/verification.md`。

## 分层

调用只能从上往下，由 `internal/archtest` 强制：

```text
main.go → internal/app        窗口、菜单、系统对话框、设置
cmd/sheaf/main.go → internal/cli  参数、工作区定位、锁、输出封装、退出码；仅依赖 appdir 和 session
          internal/appdir     GUI / CLI 共用的数据目录、工作区文件锁（不依赖其他 internal 包）
            ↓
          internal/native     界面：外框 + 需求页（track，含未提交改动）+ 差异和设置窗口 + 共用控件
            ↓ 只经 session
          internal/session    一个工作区的全部业务状态；界面能调的都在这里
            ↓
  engine  查询        index   内存提交索引      query    条件解析（纯逻辑）
  gitview 按需读 git  gitops  写操作的计划和执行  discover 发现仓库
  watch   文件变更分类
            ↓
          internal/gitcli     唯一启动 git 和 glab 的地方；写命令白名单在 record.go
```

## 建议的阅读顺序

1. `internal/native/view/view.go`：页面和外框的契约，读完就明白界面怎么组织。
2. `internal/session/types.go`：业务类型，界面看到的数据长这样。
3. `internal/query/`：最容易读的纯逻辑，配着测试看，能了解筛选规则。
4. `internal/gitcli/record.go`、`write.go`：应用到底能对你的仓库做什么。
5. 跟一遍下面两条链路。

## 两条典型链路

**写：点需求页的「刷新远端」**

```text
internal/native/track/panes.go Page.fetch        后台 goroutine 调 session，结果用 Update 交回主线程
internal/session/batch.go      Session.FetchAll  选出要 fetch 的仓库
internal/gitops/batch.go       FetchAll          逐仓库执行、跳过没有 origin 的
internal/gitcli/write.go       FetchRemote       参数固定的写函数，经 gitcli.Write 跑并记日志
```

新增写操作就按这条路走：`gitcli` 加函数和白名单 → `gitops` 写计划和执行 → `session` 暴露 → 界面调用。

**读：打开一个需求**

```text
internal/native/track/page.go  需求号成为临时聚合条件，选择后读取概览、文件和提交
internal/session/             Session.Requirements、TagAgg、TagAggFiles、TagAggCommits、Rows
internal/engine/engine.go      按条件算结果（解析在 internal/query）
internal/index/                内存索引，不调 git
```

差异不在索引里，走 `internal/gitview`，按需调 git。

CLI 从 `cmd/sheaf/main.go` 进入 `internal/cli.Main`。参数处理看 `parse.go`，命令调度和输出契约看 `main.go`，终端摘要与详情的呈现看 `output.go`；业务证据的查询接口从 `internal/session/report.go`、`report_types.go` 开始读。`workspaces` 通过 `session.KnownWorkspaces` 只读列举配置和最近记录，不打开 session；相关字段、目录隔离与无 Git 调用的测试在 `internal/cli/workspaces_test.go`。CLI 的端到端行为测试在 `internal/cli/main_test.go`，使用隔离目录和本地 Git 仓库；并发测试由子进程实际持锁。

应用打开时 CLI 仍反复计算合并或 patch 比较，先看 `internal/session/derived.go`：它负责这些纯提交事实的共享缓存和延迟保存；跨进程合并与关闭前落盘的测试在 `derived_concurrent_test.go`。

## 界面是怎么画的

- mygo 的 `ui` 是每帧重建：`Panes(c)` 这类函数每帧执行一遍，描述当前该显示什么。状态放在 `Page` 结构体里，改了状态下一帧自然变。这是读界面代码最需要先接受的一点。
- 耗时的事放 goroutine，结果用 `h.Update(func)` 交回主线程。`h.Caller()` 要在起 goroutine 之前取：它按取的先后编号，同类调用新的取消旧的。`Close` 里停计时器、取消加载。
- 工作区只有一个需求视图，「未提交」是其中的内置入口。

## 我想改 X，它在哪

先从界面文字反查：`rg -n -F '<文字>' internal/native -g '*.go' -g '!**/*_test.go'` 找到字段，再搜字段的引用。

路径相对 `internal/native/`：

| 界面区域 | 入口 |
| --- | --- |
| 工作区外框、标题栏 | `workspace.go`；视图登记在 `views.go` |
| 停靠面板、拖动尺寸 | `shell/shell.go`；布局保存在 `layout.go` |
| 状态栏 | 左栏按钮和任务角在 `workspace.go`，其余（含右栏按钮）在各视图的 `panes.go`；设置菜单项在 `internal/app/menu.go` |
| 需求页的顶栏和页签 | `track/panes.go` |
| 需求页左栏：需求、各仓库阶段、未提交改动 | `track/rail.go` |
| 需求页的差异流、仓库和文件标题、目录 | `track/flow.go`；流本身（按可见范围读取、折叠、释放）在 `diffview/stream.go` |
| 需求页的提交、打开提交差异 | `track/lists.go`、`commit.go` |
| 未提交改动的上下分栏、目录列表、需求标注、文件列表与差异流 | `track/dirty.go`；全部目录的汇总在 `internal/session/dirtyview.go`，归属计算在 `session.go`，需求号匹配在 `requirements.go` |
| 每个文件的增删行数 | `internal/session/stats.go` |
| 批量刷新远端、更新分支、创建 MR 的确认和结果 | `track/panes.go`、`track/batch.go`；规则在 `internal/gitops/batch.go`、`mr.go` |
| 改动文件树、差异 | `diffview/` |
| 差异、设置窗口 | `diffwin/`、`settingswin/` |
| 按钮、菜单、输入、Toast、确认弹层、批量确认窗、空状态 | `widgets/`；批量确认窗在 `plan.go` |
| 窗口装配、系统对话框 | `internal/app` |
| 设置 › 通用安装、卸载命令行工具和 zsh 补全 | `internal/app/clitool.go`；界面和文案在 `internal/native/settingswin/` |
| 颜色、尺寸、字体 | `theme/theme.go`；新增前先找有没有合适的 |
| 图标 | `theme/icons.go` |
| 文案 | 各包的 `words.go`，两种语言都要改；共用词在 `i18n/i18n.go` |

改行高后要看长列表的滚动、选中位置和差异行对齐。工作区窗口最小宽 1180，改面板和列宽后在窄窗口下也看一眼。

## 加一个视图

1. 新建 `internal/native/<名>`，实现 `view.View`，把 ID 和 intent 放进 `view/view.go`。
2. 在 `views.go` 的 `registry` 登记，更新 `internal/archtest` 的依赖白名单。
3. 在 `docs/product/views.md` 加 `### <视图 id>`。
4. 跳转用 `h.Go(view.Track, view.TempIntent{Branch: ...})`，共用界面放公共包。

## 测试拦住你时

| 测试 | 常见原因 | 修法 |
| --- | --- | --- |
| `TestNativeHygiene` | 控件里写了字面文案，或在 `theme` 外写颜色 | 文案进 i18n 表，颜色用 `theme` |
| `TestNativeWords` | 中英文缺字段或格式符不一致 | 补全两份表 |
| `TestDependencies` | 页面互相 import，或界面直接依赖 git 包 | 共用部分移到公共包，业务经 `session` |
| `TestOnlyGitcliRunsGit`、`TestWritesOnlyInGitcli` | 在 `gitcli` 外启动 git 或写 | 按上面的写链路扩展 |
| `TestWriteWhitelist` | 加了写命令没登记 | 改 `gitcli/record.go` 和测试 |
| `TestIA` | 增删视图或窗口 | 改 `docs/product/views.md` |

## 排错

- **写命令做了什么**：应用菜单「操作记录」，文件在 `~/.sheaf-dev/workspaces/<id>/commands.jsonl`（正式版根目录为 `~/.sheaf`；`SHEAF_HOME` 可覆盖）。
- **怀疑索引有问题**：删掉 `~/.sheaf-dev/cache/workspaces/<id>/index.bin` 再启动，会重建；正式版改用 `~/.sheaf/cache/`。
- **看到旧状态**：先退出已有实例；进程被强杀后，下次启动可能恢复上次的工作区。
- **参考测试**：视图测试看 `internal/native/track/page_test.go`，写操作看 `internal/gitops/*_test.go`，都在临时目录里造仓库。

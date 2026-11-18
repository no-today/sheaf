package track

import "sheaf/internal/native/i18n"

// words are the page's texts; {name} is filled in by fill.
type words struct {
	Filters           string
	ShowOutline       string
	HideOutline       string
	Uncommitted       string
	WorktreeSection   string
	ReviewChanged     string
	ReviewUpdating    string
	ReviewFailed      string
	ReviewHint        string
	UncommittedHint   string
	TempHint          string
	Unsaved           string
	None              string
	Intro             string
	IntroHint         string
	IntroPattern      string
	IntroSettings     string
	NoneHint          string
	Loading           string
	BuildingIndex     string
	BuildingIndexHint string
	Error             string
	Empty             string
	EmptyHint         string
	ReqEmpty          string
	ReqEmptyHint      string
	Master            string
	MasterHint        string
	DiffLabel         string
	DiffHint          string
	DiffBase          string
	DiffWorktree      string
	DiffStageHint     string
	CompareBase       string
	TargetRepos       string
	TargetHead        string
	Favs              string
	TargetFoot        string
	Fav               string
	Unfav             string
	BranchSearch      string
	More              string
	NonePicked        string
	TabFiles          string
	AddedFile         string
	ModifiedFile      string
	DeletedFile       string
	ConflictFile      string
	OnlyDir           string
	WcFileCount       string
	TabCommits        string
	AllChanges        string
	OnlyWc            string
	DownHead          string
	DownHint          string
	Scope             string
	Unfocus           string
	MergesHidden      string
	MergesHiddenTitle string
	MergesShown       string
	Resolved          string
	ResolvedHint      string
	Show              string
	MainDir           string
	Manual            string
	BranchesHead      string
	NoCandidates      string
	UsedBy            string
	AutoMark          string
	BackToAuto        string
	Merged            string
	NoBranch          string
	SkipBaseUnset     string
	SkipNoBase        string
	NoFiles           string
	NoFilesHint       string
	NoCommits         string
	NoCommitsHint     string
	NoDirty           string
	WcMeta            string
	WcDirs            string
	WcBase            string
	WcCond            string
	WcCondValue       string
	WcScopeAll        string
	OnMain            string
	OnMainHint        string
	JustNow           string
	PickedDirs        string
	WcBadge           string
	NewBadge          string
	MrPromise         string
	PlanSkipped       string
	PlanPick          string
	PlanRunning       string
	ResSkipped        string
	ResFailed         string
	SumUpToDate       string
	SumFailed         string
	SumSkipped        string
	Close             string
	Merging           string
	MergingHint       string
	MrHint            string
	MrTitle           string
	MrHowOpen         string
	MrHowPush         string
	MrGo              string
	MrGhMissing       string
	MrGhAuthHint      string
	MrGhResAuth       string
	MrBatchTitle      string
	MrBatchGo         string
	MrGlabMissing     string
	MrSkipNoBranch    string
	MrSkipNoTarget    string
	MrSkipIsTarget    string
	MrSkipNoRemote    string
	MrSkipNoProject   string
	MrSkipDiverged    string
	MrShortNoBranch   string
	MrShortNoTarget   string
	MrShortIsTarget   string
	MrShortNoRemote   string
	MrShortNoProject  string
	MrShortDiverged   string
	MrBriefOpen       string
	MrBriefPush       string
	MrBriefMissing    string
	SyncBriefRef      string
	SyncBriefWorktree string
	SyncBriefNoLocal  string
	SyncBriefNoRemote string
	SyncBriefMerging  string
	SyncBriefDirty    string
	SyncBriefUnpushed string
	MrResCreated      string
	MrResExists       string
	MrResAuth         string
	MrAuthHint        string
	MrDone            string
	MrSumCreated      string
	MrSumExisting     string
	MrFailedToast     string
	SyncAction        string
	SyncHint          string
	SyncTitle         string
	SyncPromise       string
	SyncGo            string
	SyncNoFavs        string
	SyncFailedToast   string
	SyncDone          string
	SyncSumUpdated    string
	SyncHowRef        string
	SyncHowWorktree   string
	SyncSkipNoLocal   string
	SyncSkipNoRemote  string
	SyncSkipMerging   string
	SyncSkipDirty     string
	SyncSkipUnpushed  string
	FailAuth          string
	FailNetwork       string
	FailTop           string
	Commits           string
	SwitchBranch      string
	OpenDiff          string
	FoldFile          string
	UnfoldFile        string
	FoldAll           string
	UnfoldAll         string
	FoldAllHint       string
	CopyPath          string
	WcTag             string
	NewFile           string
	StreamEnd         string
	StreamEndWc       string
	StreamEndResolved string
	StreamEndStop     string
	FindReq           string
	FindText          string
	Rename            string
	RenameLabel       string
	TrackHint         string
	Trackable         string
	Track             string
	TrackLabel        string
	Untrack           string
	Untracked         string
	StageDev          string
	StageReading      string
	StageUnknown      string
	StageLanded       string
	StageArchived     string
	StageAwaiting     string
	Archive           string
	Restore           string
	ArchivedToast     string
	RestoredToast     string
	InRepos           string
	ReposPane         string
	ReposPaneEmpty    string
	RowDirty          string
	RowMR             string
	Fetch             string
	FetchHint         string
	MrCount           string
	MrAction          string
	DiffNone          string
	DiscardFile       string
	DeleteFile        string
	DiscardTitle      string
	DiscardDetail     string
	DiscardOK         string
	DeleteTitle       string
	DeleteDetail      string
	DeleteOK          string
	Discarded         string
	Deleted           string
	DiscardFailed     string
	ActNeedReq        string
	ActNoRows         string
	ActNoTicked       string
	FetchFailed       string
	FetchSumDone      string
	FetchSumNoRemote  string
	TabChanges        string
	ScopeLabel        string
	ScopeHint         string
	BackToOwn         string
}

var tr = i18n.New(words{
	Filters:           "需求",
	ShowOutline:       "显示右栏",
	HideOutline:       "隐藏右栏",
	Uncommitted:       "未提交",
	UncommittedHint:   "不属于任何关注需求的改动",
	TempHint:          "临时结果",
	Unsaved:           "临时",
	None:              "还没有关注的需求",
	Intro:             "一个需求号，看全所有仓库的改动",
	IntroHint:         "需求号就是工单号，如 ABC-123。Sheaf 从分支名和你近 90 天的提交里找。找不到？检查需求号写法。",
	IntroPattern:      "当前写法：{pattern}",
	IntroSettings:     "修改写法",
	NoneHint:          "在左栏输入需求号开始关注",
	Loading:           "加载中…",
	BuildingIndex:     "正在读取提交",
	BuildingIndexHint: "读完就能查看",
	Error:             "无法读取：{reason}",
	Empty:             "这里没有相关提交",
	EmptyHint:         "在左栏搜索需求号或提交说明",
	ReqEmpty:          "所有仓库都没有 {id} 的提交",
	ReqEmptyHint:      "分支名或提交说明里要带上需求号",
	Master:            "对比分支",
	MasterHint:        "改动都和它比，MR 也合到它；全工作区共用",
	DiffLabel:         "查看",
	DiffHint:          "看全部、未提交，或某阶段还没合入的",
	DiffBase:          "全部改动",
	DiffWorktree:      "未提交",
	DiffStageHint:     "只看还没合进这一阶段的提交",
	CompareBase:       "各仓库主分支",
	TargetRepos:       "{n} 个仓库有",
	TargetHead:        "对比分支 · 收藏的在前",
	Favs:              "收藏",
	TargetFoot:        "没有该分支的仓库会标出来。收藏和选择按工作区保存。",
	Fav:               "收藏",
	Unfav:             "取消收藏",
	BranchSearch:      "搜索分支，如 ABC-123",
	More:              "还有 {n} 个，输入以筛选",
	NonePicked:        "未选择",
	TabFiles:          "文件",
	AddedFile:         "新增",
	ModifiedFile:      "修改",
	DeletedFile:       "删除",
	ConflictFile:      "冲突",
	OnlyDir:           "只在 {where}",
	WcFileCount:       "{n} 个文件",
	TabCommits:        "提交",
	AllChanges:        "全部改动",
	OnlyWc:            "未提交",
	DownHead:          "{b} 比 {branch} 多的 {n} 个提交",
	DownHint:          "查看 {b} 多出的 {n} 个提交",
	Scope:             "范围",
	Unfocus:           "显示全部",
	MergesHidden:      "已隐藏 {n} 个无冲突合并，点此显示",
	MergesHiddenTitle: "已隐藏合并",
	MergesShown:       "已显示全部合并，点此隐藏无冲突的",
	Resolved:          "手动合并",
	ResolvedHint:      "合并时解决过冲突或改过文件",
	Show:              "显示",
	WorktreeSection:   "未提交 · {location} · {n} 个文件",
	ReviewChanged:     "内容已变化 · 刷新",
	ReviewUpdating:    "更新中",
	ReviewFailed:      "读取失败 · 重试",
	ReviewHint:        "阅读时保留当前差异；点击读取最新内容，离开差异区域后自动更新",
	MainDir:           "主目录",
	Manual:            "手动",
	BranchesHead:      "{repo} 的分支 · 按相关提交数排序",
	NoCandidates:      "没有相关分支",
	UsedBy:            "已在第 {n} 行",
	AutoMark:          "自动",
	BackToAuto:        "恢复自动（{b}）",
	Merged:            "已合入 {base}",
	NoBranch:          "无分支",
	SkipBaseUnset:     "未设主分支",
	SkipNoBase:        "没有 {base}",
	NoFiles:           "没有改动",
	NoFilesHint:       "换个范围，或选其他行看看",
	NoCommits:         "没有提交",
	NoCommitsHint:     "选其他行，或放宽筛选",
	NoDirty:           "都已提交",
	WcDirs:            "工作目录",
	WcMeta:            "{repos} 个仓库 · {files} 个文件",
	WcBase:            "上次提交",
	WcCond:            "工作目录",
	WcCondValue:       "有未提交改动",
	WcScopeAll:        "点一行只看它",
	OnMain:            "在主分支上",
	OnMainHint:        "直接在主分支上改，没开分支",
	JustNow:           "刚刚",
	PickedDirs:        "已选 {n} 个目录",
	WcBadge:           "未提交",
	NewBadge:          "新文件",
	MrPromise:         "标题取自提交；按需推送，不强推；已有的只给链接。",
	PlanSkipped:       "将跳过 {n} 个",
	PlanPick:          "处理 {repo} · {branch}",
	PlanRunning:       "正在处理 {n} 个…",
	ResSkipped:        "跳过 · {why}",
	ResFailed:         "✕ 失败",
	SumUpToDate:       "{n} 个已是最新",
	SumFailed:         "{n} 个失败",
	SumSkipped:        "跳过 {n} 个",
	Close:             "关闭",
	Merging:           "合并中 · {n} 个冲突",
	MergingHint:       "合并没做完，请在 IDE 或 Coding Agent 里解决",
	MrHint:            "为勾选的行开 MR/PR，合到对比分支",
	MrTitle:           "为 {n} 个分支创建 MR",
	MrHowOpen:         "创建 {b} → {t} 的 MR",
	MrHowPush:         "先推送 {b}，再创建 {b} → {t} 的 MR",
	MrGo:              "创建 {n} 个 MR",
	MrGhMissing:       "未安装 gh：brew install gh 后 gh auth login",
	MrGhAuthHint:      "gh 未登录：在终端运行 gh auth login",
	MrGhResAuth:       "✕ gh 未登录",
	MrBatchTitle:      "为 {n} 个分支创建 {kind}",
	MrBatchGo:         "创建 {n} 个 {kind}",
	MrGlabMissing:     "未安装 glab：brew install glab 后 glab auth login",
	MrSkipNoBranch:    "没有需求分支，跳过",
	MrSkipNoTarget:    "找不到对比分支，跳过",
	MrSkipIsTarget:    "{b} 就是目标分支，跳过",
	MrSkipNoRemote:    "没有远端，跳过",
	MrSkipNoProject:   "远端地址认不出项目，跳过",
	MrSkipDiverged:    "本地和远端各有新提交，请先在终端处理",
	MrShortNoBranch:   "没有分支",
	MrShortNoTarget:   "没有目标分支",
	MrShortIsTarget:   "是目标分支",
	MrShortNoRemote:   "没有远端",
	MrShortNoProject:  "认不出项目",
	MrShortDiverged:   "和远端对不上",
	MrBriefOpen:       "创建 {kind}",
	MrBriefPush:       "推送并创建 {kind}",
	MrBriefMissing:    "未安装 {tool}",
	SyncBriefRef:      "更新",
	SyncBriefWorktree: "在 {wt} 更新",
	SyncBriefNoLocal:  "没有本地分支",
	SyncBriefNoRemote: "远端没有",
	SyncBriefMerging:  "{wt} 操作没做完",
	SyncBriefDirty:    "{wt} 有未提交改动",
	SyncBriefUnpushed: "有未推送的提交",
	MrResCreated:      "已创建 {mr}",
	MrResExists:       "已有 {mr}",
	MrResAuth:         "✕ glab 未登录",
	MrAuthHint:        "glab 未登录：在终端运行 glab auth login",
	MrDone:            "MR 已处理",
	MrSumCreated:      "{n} 个 MR 已创建",
	MrSumExisting:     "{n} 个已有 MR",
	MrFailedToast:     "创建 MR/PR 失败：{reason}",
	SyncAction:        "更新分支",
	SyncHint:          "把收藏分支更新到远端最新，先看计划再确认",
	SyncTitle:         "更新 {n} 个收藏分支",
	SyncPromise:       "不切分支、不合并、不碰没提交的目录。",
	SyncGo:            "更新 {n} 个分支",
	SyncNoFavs:        "先收藏分支，并确认仓库里有它",
	SyncFailedToast:   "更新失败：{reason}",
	SyncDone:          "更新完成",
	SyncSumUpdated:    "已更新 {n} 个分支",
	SyncHowRef:        "把 {b} 更新到 {remote}",
	SyncHowWorktree:   "在 {wt} 把 {b} 更新到 {remote}",
	SyncSkipNoLocal:   "本地没有 {b}，跳过",
	SyncSkipNoRemote:  "远端没有 {b}，跳过",
	SyncSkipMerging:   "{wt} 有操作没做完，跳过",
	SyncSkipDirty:     "{wt} 有 {n} 处未提交改动，跳过",
	SyncSkipUnpushed:  "有提交没推送，跳过",
	FailAuth:          "登录失败：检查 git 账号密码。远端地址与凭据的 http/https 要一致",
	FailNetwork:       "连不上：检查网络或 VPN",
	FailTop:           "主要原因：{reason}（{n} 个）",
	Commits:           "{n} 个提交",
	SwitchBranch:      "切换 {repo} 的分支",
	OpenDiff:          "在新窗口查看",
	FoldFile:          "折叠",
	UnfoldFile:        "展开",
	FoldAll:           "全部折叠",
	UnfoldAll:         "全部展开",
	FoldAllHint:       "（⌥ 点击：全部文件）",
	CopyPath:          "复制路径",
	WcTag:             "含未提交",
	NewFile:           "新文件",
	StreamEnd:         "共 {n} 个文件",
	StreamEndWc:       "，其中 {n} 个含未提交改动",
	StreamEndResolved: "，{n} 个合并时改过",
	StreamEndStop:     "。",
	FindReq:           "搜索需求号或提交说明",
	FindText:          "搜索提交说明“{q}”",
	Rename:            "重命名",
	RenameLabel:       "需求名称",
	TrackHint:         "输入需求号开始关注",
	Trackable:         "可关注",
	Track:             "关注",
	TrackLabel:        "关注 {id}",
	Untrack:           "取消关注",
	Untracked:         "已取消关注 {id}",
	StageDev:          "开发中",
	StageReading:      "读取中",
	StageUnknown:      "无法确认",
	StageLanded:       "已合完",
	StageArchived:     "已归档",
	StageAwaiting:     "待合 {branch}",
	Archive:           "归档",
	Restore:           "恢复",
	ArchivedToast:     "已归档 {id}",
	RestoredToast:     "已恢复 {id}",
	InRepos:           "{name} 在各仓库",
	ReposPane:         "各仓库进度",
	ReposPaneEmpty:    "选个需求，看它在各仓库的进度",
	RowDirty:          "{n} 个未提交",
	RowMR:             "{mr}",
	Fetch:             "刷新远端",
	FetchHint:         "拉下远端最新信息，不改任何分支",
	MrCount:           "创建 MR/PR · {n}",
	MrAction:          "创建 MR/PR",
	DiffNone:          "无改动",
	DiscardFile:       "还原改动…",
	DeleteFile:        "删除文件…",
	DiscardTitle:      "还原 {name}？",
	DiscardDetail:     "所有改动都会丢掉，回到上次提交。无法撤销。",
	DiscardOK:         "还原",
	DeleteTitle:       "删除 {name}？",
	DeleteDetail:      "新文件会被彻底删除，无法撤销。",
	DeleteOK:          "删除",
	Discarded:         "已还原 {name}",
	Deleted:           "已删除 {name}",
	DiscardFailed:     "还原失败：{reason}",
	ActNeedReq:        "先选一个需求",
	ActNoRows:         "这个需求还没有分支",
	ActNoTicked:       "分支都已合并，没有要处理的",
	FetchFailed:       "刷新失败：{reason}",
	FetchSumDone:      "已刷新 {n} 个仓库",
	FetchSumNoRemote:  "{n} 个没有远端",
	TabChanges:        "改动",
	ScopeLabel:        "范围",
	ScopeHint:         "全部改动，或只看没提交的",
	BackToOwn:         "回到提交",
}, words{
	Filters:           "Requirements",
	ShowOutline:       "Show right pane",
	HideOutline:       "Hide right pane",
	Uncommitted:       "Uncommitted",
	UncommittedHint:   "Changes outside any followed requirement",
	TempHint:          "Temporary result",
	Unsaved:           "Temporary",
	None:              "Not following any requirements yet",
	Intro:             "One requirement number, every repo's changes",
	IntroHint:         "A requirement number is a ticket ID, like ABC-123. Sheaf looks in branch names and your last 90 days of commits. Nothing found? Check the pattern.",
	IntroPattern:      "Current pattern: {pattern}",
	IntroSettings:     "Edit Pattern",
	NoneHint:          "Type a requirement number on the left to follow it",
	Loading:           "Loading…",
	BuildingIndex:     "Reading commits",
	BuildingIndexHint: "Ready once reading finishes",
	Error:             "Couldn't read it: {reason}",
	Empty:             "No matching commits here",
	EmptyHint:         "Search a requirement or commit message on the left",
	ReqEmpty:          "No repo has commits for {id}",
	ReqEmptyHint:      "Put the number in branch names or commit messages",
	Master:            "Compare to",
	MasterHint:        "Changes and MRs compare to it; shared by the workspace",
	DiffLabel:         "View",
	DiffHint:          "All, uncommitted, or not yet merged into a stage",
	DiffBase:          "All changes",
	DiffWorktree:      "Uncommitted",
	DiffStageHint:     "Only commits not yet merged into this stage",
	CompareBase:       "Each repo's main branch",
	TargetRepos:       "in {n} repos",
	TargetHead:        "Compare branch · favourites first",
	Favs:              "Favourites",
	TargetFoot:        "Repos without the branch are marked. Favourites and picks are saved per workspace.",
	Fav:               "Favourite",
	Unfav:             "Remove favourite",
	BranchSearch:      "Search branches, e.g. ABC-123",
	More:              "{n} more; type to filter",
	NonePicked:        "None ticked",
	TabFiles:          "Files",
	AddedFile:         "Added",
	ModifiedFile:      "Modified",
	DeletedFile:       "Deleted",
	ConflictFile:      "Conflict",
	OnlyDir:           "Only in {where}",
	WcFileCount:       "{n} file|{n} files",
	TabCommits:        "Commits",
	AllChanges:        "All changes",
	OnlyWc:            "Uncommitted",
	DownHead:          "{n} commits in {b} not in {branch}",
	DownHint:          "See the {n} commits only in {b}",
	Scope:             "Scope",
	Unfocus:           "Show all",
	MergesHidden:      "{n} clean merge hidden. Click to show|{n} clean merges hidden. Click to show",
	MergesHiddenTitle: "Merges hidden",
	MergesShown:       "All merges shown. Click to hide clean ones",
	Resolved:          "Manual merge",
	ResolvedHint:      "Conflicts resolved or files edited while merging",
	Show:              "Show",
	WorktreeSection:   "Uncommitted · {location} · {n} file|Uncommitted · {location} · {n} files",
	ReviewChanged:     "Changes available · Refresh",
	ReviewUpdating:    "Updating",
	ReviewFailed:      "Read failed · Retry",
	ReviewHint:        "Keep the current diff while reading. Refresh to load changes, or leave the diff to update automatically.",
	MainDir:           "main dir",
	Manual:            "manual",
	BranchesHead:      "Branches of {repo} · by related commits",
	NoCandidates:      "No related branches",
	UsedBy:            "on row {n}",
	AutoMark:          "auto",
	BackToAuto:        "Back to automatic ({b})",
	Merged:            "In {base}",
	NoBranch:          "No branch",
	SkipBaseUnset:     "No main branch",
	SkipNoBase:        "No {base}",
	NoFiles:           "No changes",
	NoFilesHint:       "Try another scope or another row",
	NoCommits:         "No commits",
	NoCommitsHint:     "Pick another row or loosen the filter",
	NoDirty:           "All committed",
	WcDirs:            "Working directories",
	WcMeta:            "{repos} repos · {files} files",
	WcBase:            "Last commit",
	WcCond:            "Working directory",
	WcCondValue:       "has uncommitted changes",
	WcScopeAll:        "Click a row to see only it",
	OnMain:            "On the main branch",
	OnMainHint:        "Edited on the main branch, no branch of its own",
	JustNow:           "just now",
	PickedDirs:        "{n} directories selected",
	WcBadge:           "Uncommitted",
	NewBadge:          "New file",
	MrPromise:         "Titles come from commits; pushes if needed, never forces; open ones just get a link.",
	PlanSkipped:       "{n} skipped",
	PlanPick:          "Run {repo} · {branch}",
	PlanRunning:       "Running {n}…",
	ResSkipped:        "Skipped · {why}",
	ResFailed:         "✕ Failed",
	SumUpToDate:       "{n} up to date",
	SumFailed:         "{n} failed",
	SumSkipped:        "{n} skipped",
	Close:             "Close",
	Merging:           "Merging · {n} conflict|Merging · {n} conflicts",
	MergingHint:       "Merge unfinished: resolve it in your IDE or a Coding Agent",
	MrHint:            "Open MR/PRs for the ticked rows into the compare branch",
	MrTitle:           "Create an MR for {n} branch|Create MRs for {n} branches",
	MrHowOpen:         "Open an MR {b} → {t}",
	MrHowPush:         "Push {b}, then open an MR {b} → {t}",
	MrGo:              "Create {n} MR|Create {n} MRs",
	MrGhMissing:       "gh not installed: brew install gh, then gh auth login",
	MrGhAuthHint:      "gh not signed in: run gh auth login in Terminal",
	MrGhResAuth:       "✕ gh not signed in",
	MrBatchTitle:      "Create {kind} for {n} branch|Create {kind} for {n} branches",
	MrBatchGo:         "Create {n} {kind}",
	MrGlabMissing:     "glab not installed: brew install glab, then glab auth login",
	MrSkipNoBranch:    "No requirement branch, skipped",
	MrSkipNoTarget:    "No compare branch found, skipped",
	MrSkipIsTarget:    "{b} is the target branch, skipped",
	MrSkipNoRemote:    "No remote, skipped",
	MrSkipNoProject:   "Can't tell the project from the remote URL, skipped",
	MrSkipDiverged:    "Local and remote both have new commits; fix it in Terminal first",
	MrShortNoBranch:   "no branch",
	MrShortNoTarget:   "no target",
	MrShortIsTarget:   "is the target",
	MrShortNoRemote:   "no remote",
	MrShortNoProject:  "unknown project",
	MrShortDiverged:   "out of step with remote",
	MrBriefOpen:       "Open {kind}",
	MrBriefPush:       "Push, then open {kind}",
	MrBriefMissing:    "{tool} not installed",
	SyncBriefRef:      "Update",
	SyncBriefWorktree: "Update in {wt}",
	SyncBriefNoLocal:  "No local branch",
	SyncBriefNoRemote: "Not on remote",
	SyncBriefMerging:  "{wt} is busy",
	SyncBriefDirty:    "{wt} has uncommitted changes",
	SyncBriefUnpushed: "Unpushed commits",
	MrResCreated:      "created {mr}",
	MrResExists:       "open {mr}",
	MrResAuth:         "✕ glab not signed in",
	MrAuthHint:        "glab not signed in: run glab auth login in Terminal",
	MrDone:            "MRs done",
	MrSumCreated:      "{n} MRs created",
	MrSumExisting:     "{n} already open",
	MrFailedToast:     "Create MR/PR failed: {reason}",
	SyncAction:        "Update Branches",
	SyncHint:          "Bring favourite branches up to date with the remote; review the plan first",
	SyncTitle:         "Update {n} favourite branch|Update {n} favourite branches",
	SyncPromise:       "No branch switching, no merging, uncommitted folders left alone.",
	SyncGo:            "Update {n} branch|Update {n} branches",
	SyncNoFavs:        "Favourite a branch first, and check the repo has it",
	SyncFailedToast:   "Update failed: {reason}",
	SyncDone:          "Update done",
	SyncSumUpdated:    "{n} branch updated|{n} branches updated",
	SyncHowRef:        "Update {b} to {remote}",
	SyncHowWorktree:   "In {wt}, update {b} to {remote}",
	SyncSkipNoLocal:   "No local {b}, skipped",
	SyncSkipNoRemote:  "No {b} on the remote, skipped",
	SyncSkipMerging:   "{wt} has an unfinished operation, skipped",
	SyncSkipDirty:     "{wt} has {n} uncommitted change: skipped|{wt} has {n} uncommitted changes: skipped",
	SyncSkipUnpushed:  "Unpushed commits, skipped",
	FailAuth:          "Sign-in failed: check your git credentials. Remote and credentials must both use http or https",
	FailNetwork:       "Can't connect: check the network or VPN",
	FailTop:           "Main cause: {reason} ({n})",
	Commits:           "{n} commit|{n} commits",
	SwitchBranch:      "Switch the branch of {repo}",
	OpenDiff:          "Open in New Window",
	FoldFile:          "Fold",
	UnfoldFile:        "Expand",
	FoldAll:           "Fold All",
	UnfoldAll:         "Expand All",
	FoldAllHint:       " (⌥-click: all files)",
	CopyPath:          "Copy Path",
	WcTag:             "uncommitted",
	NewFile:           "new file",
	StreamEnd:         "{n} file in total|{n} files in total",
	StreamEndWc:       ", {n} with uncommitted changes",
	StreamEndResolved: ", {n} edited while merging",
	StreamEndStop:     ".",
	FindReq:           "Search requirements or commits",
	FindText:          "Search commit messages for “{q}”",
	Rename:            "Rename",
	RenameLabel:       "Requirement name",
	TrackHint:         "Type a requirement number to follow it",
	Trackable:         "Can follow",
	Track:             "Follow",
	TrackLabel:        "Follow {id}",
	Untrack:           "Unfollow",
	Untracked:         "Unfollowed {id}",
	StageDev:          "In progress",
	StageReading:      "Reading",
	StageUnknown:      "Unconfirmed",
	StageLanded:       "Fully merged",
	StageArchived:     "Archived",
	StageAwaiting:     "Awaiting {branch}",
	Archive:           "Archive",
	Restore:           "Restore",
	ArchivedToast:     "Archived {id}",
	RestoredToast:     "Restored {id}",
	InRepos:           "{name} across repos",
	ReposPane:         "Repo progress",
	ReposPaneEmpty:    "Pick a requirement to see its progress in each repo",
	RowDirty:          "{n} uncommitted",
	RowMR:             "{mr}",
	Fetch:             "Refresh Remotes",
	FetchHint:         "Get the latest from remotes; no branch changes",
	MrCount:           "Create MR/PR · {n}",
	MrAction:          "Create MR/PR",
	DiffNone:          "No changes",
	DiscardFile:       "Discard Changes…",
	DeleteFile:        "Delete File…",
	DiscardTitle:      "Discard changes to {name}?",
	DiscardDetail:     "All changes are lost and the file goes back to the last commit. This can't be undone.",
	DiscardOK:         "Discard",
	DeleteTitle:       "Delete {name}?",
	DeleteDetail:      "This new file will be deleted for good. This can't be undone.",
	DeleteOK:          "Delete",
	Discarded:         "Discarded the changes to {name}",
	Deleted:           "Deleted {name}",
	DiscardFailed:     "Couldn't discard: {reason}",
	ActNeedReq:        "Pick a requirement first",
	ActNoRows:         "This requirement has no branches yet",
	ActNoTicked:       "All branches are merged; nothing to do",
	FetchFailed:       "Refresh failed: {reason}",
	FetchSumDone:      "{n} repo refreshed|{n} repos refreshed",
	FetchSumNoRemote:  "{n} without a remote",
	TabChanges:        "Changes",
	ScopeLabel:        "scope",
	ScopeHint:         "All changes, or only uncommitted ones",
	BackToOwn:         "back to commits",
})

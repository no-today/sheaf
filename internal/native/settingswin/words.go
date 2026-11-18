package settingswin

import "sheaf/internal/native/i18n"

type words struct {
	TabGeneral, TabMe, TabBase, TabRepos, TabIndex, TabGlab string

	ReposHead, ReposHint, RepoTrack, RepoInner, RepoSearch, RepoCount, RepoOn, RepoOff, RepoNoMatch string

	About, AppVersion                        string
	Lang, LangHint, LangAuto, LangZh, LangEn string
	CodeFont, CodeFontHint                   string

	CLIHead, CLICommand, CLIHint, CLIInstall, CLIUninstall string
	CLIInstalled, CLINotInstalled, CLIForeign, CLIFailed   string

	Workspace string
	Loading   string

	MeHead, FromGit, FromRepos, FromReposMore, Manual, AddEmail, EmailPh, Add string
	EmailInvalid, EmailDup, RemoveEmail, EmailRemoved, MeHint                 string
	OtherEmails, OtherEmailsHint                                              string
	ReqHead, ReqPattern, ReqPatternHint, ReqInvalid, ReqHint                  string

	GlabHead, GlabCopied, GlabDir, GlabDirHint, GlabPh, GlabMissing, GlabLogin, GlabHint, Copy string

	GhHead, GhHint, GhLogin, GhCommand, GhCopy string

	ColBase, BaseAuto, BaseHint, BaseFoot, BaseMainDir, BaseDirs, NoRepos                        string
	TargetsHint, Master, CompareBranch, CompareBranchField, MasterHint, MasterPh, BasesHead      string
	FlowHead, FlowHint, FlowName, FlowBranch, FlowAdd, FlowUp, FlowDown, FlowRemove, FlowRemoved string
	FlowNameHead, FlowBranchHead, FlowBaseline                                                   string
	BaselineOnly, BaselineOnlyRepo                                                               string

	IndexLine, IndexNoTime, IndexUpdating, IndexProgress, IndexError, IndexSub string
	// IndexFailed names the repos the last update could not read; IndexFailedRepos counts them ("{n} 个仓库"|"{n} repositories").
	IndexFailed                                                                     string
	IndexFailedRepos                                                                string
	Refresh, Refreshing, Refreshed                                                  string
	IndexHead, LocalData, DataDir, Reveal, CmdLog, CmdLogSub, CmdLogOpen, IndexHint string

	AgoNow, AgoMin, AgoHour, AgoDay string
}

var tr = i18n.New(words{
	TabGeneral: "通用",
	TabMe:      "我的提交",
	TabBase:    "分支",
	TabRepos:   "仓库",
	TabIndex:   "数据",
	TabGlab:    "MR/PR",

	About:        "关于",
	AppVersion:   "Sheaf %s",
	Lang:         "语言",
	CodeFont:     "代码字体",
	CodeFontHint: "改动里的代码用这个字体，没装就用 Geist Mono",
	LangHint:     "系统不是中文时显示英文",
	LangAuto:     "跟随系统",
	LangZh:       "简体中文",
	LangEn:       "English",

	CLIHead:         "命令行工具",
	CLICommand:      "sheaf 命令",
	CLIHint:         "在终端里查需求进度，含 zsh 补全",
	CLIInstall:      "安装",
	CLIUninstall:    "卸载",
	CLIInstalled:    "已安装到 %s",
	CLINotInstalled: "未安装",
	CLIForeign:      "%s 已被其他文件或链接占用，请先手动移除。",
	CLIFailed:       "无法安装或卸载命令行工具：%v",

	Workspace: "工作区",
	Loading:   "正在读取…",

	OtherEmails:     "其他邮箱",
	OtherEmailsHint: "git 配置里没有的邮箱",
	MeHead:          "我的邮箱",
	FromGit:         "自动 · 来自 git 全局配置",
	FromRepos:       "自动 · 来自 %s 的仓库配置",
	FromReposMore:   "自动 · 来自 %s 等 %d 个仓库的配置",
	Manual:          "手动添加",
	AddEmail:        "+ 添加邮箱",
	EmailPh:         "name@example.com",
	Add:             "添加",
	EmailInvalid:    "邮箱格式不对",
	EmailDup:        "已添加过",
	RemoveEmail:     "移除 %s",
	EmailRemoved:    "已移除 %s",
	MeHint:          "这些邮箱的提交都算「我」的。",
	ReqHead:         "需求号写法",
	ReqPattern:      "写法",
	ReqPatternHint:  "填正则，留空用默认",
	ReqInvalid:      "正则写错了，没有保存",
	ReqHint:         "要和团队编号一致，如 ABC-123。",

	GlabHead:    "GitLab（glab）",
	GlabCopied:  "已复制，粘到终端运行",
	GlabDir:     "glab 配置目录",
	GlabDirHint: "留空则用系统的 glab 登录",
	GlabPh:      "留空，或 ~/glab/公司A",
	GlabMissing: "目录不存在，请先创建（可用 ~）",
	GlabLogin:   "登录该目录",
	GlabHint:    "只对本工作区生效。先建目录，再运行下面的命令，<host> 换成你的 GitLab 域名。GITLAB_TOKEN 仍然有效。",
	Copy:        "复制",
	GhHead:      "GitHub（gh）",
	GhHint:      "用系统 gh 的登录给 GitHub 开 PR",
	GhLogin:     "登录 GitHub",
	GhCommand:   "gh auth login",
	GhCopy:      "复制登录命令",

	TargetsHint:        "「全部改动」和它比，进度按合并流程算。",
	FlowHead:           "合并流程",
	FlowHint:           "按顺序列出要合进的分支。留空用主分支，合完手动归档。",
	FlowName:           "第 %d 阶段名称",
	FlowBranch:         "第 %d 阶段分支",
	FlowAdd:            "+ 添加阶段",
	FlowUp:             "上移第 %d 阶段",
	FlowDown:           "下移第 %d 阶段",
	FlowRemove:         "删除第 %d 阶段",
	FlowRemoved:        "已删除阶段",
	FlowNameHead:       "阶段名称",
	FlowBranchHead:     "目标分支",
	FlowBaseline:       "终点分支：%s。",
	BaselineOnly:       "只看终点",
	BaselineOnlyRepo:   "%s 只看终点",
	Master:             "对比分支",
	CompareBranch:      "分支",
	CompareBranchField: "对比分支输入框",
	MasterHint:         "改动、↑↓ 和 MR/PR 都以它为准",
	MasterPh:           "留空用各仓库主分支",
	BasesHead:          "各仓库主分支",
	BaseMainDir:        "只有主目录",
	BaseDirs:           "另有 %d 个工作目录",
	ColBase:            "主分支",
	BaseAuto:           "自动",
	BaseHint:           "选择分支",
	BaseFoot:           "自动按 origin/master、origin/main、master、main 找（%d 个仓库）。缺这一阶段的仓库会跳过。",
	NoRepos:            "还没有找到仓库",
	ReposHead:          "包含的仓库",
	ReposHint:          "共 %d 个，%d 个不包含。不包含的不出现在需求页。",
	RepoTrack:          "包含 %s",
	RepoInner:          "内含 %d 个仓库",
	RepoSearch:         "搜索仓库",
	RepoCount:          "已包含 %d / %d",
	RepoOn:             "已包含",
	RepoOff:            "未包含",
	RepoNoMatch:        "没有匹配的仓库",

	IndexLine:        "%s 个提交 · %s更新",
	IndexNoTime:      "%s 个提交",
	IndexUpdating:    "%s 个提交 · 正在更新…",
	IndexProgress:    "正在读取 %d / %d",
	IndexError:       "读取失败：%s",
	IndexFailed:      "%s读取失败：%s",
	IndexFailedRepos: "{n} 个仓库",
	IndexSub:         "%d 个仓库、%d 个工作目录，有改动自动更新",
	Refresh:          "刷新",
	Refreshing:       "正在刷新…",
	Refreshed:        "已是最新",
	DataDir:          "数据位置",
	Reveal:           "在 Finder 中显示",
	CmdLog:           "操作记录",
	CmdLogSub:        "每次改动仓库的操作，保留 30 天",
	CmdLogOpen:       "查看记录",
	IndexHead:        "提交数据",
	LocalData:        "本地数据",
	IndexHint:        "数据只存在本机，不写进你的仓库。",

	AgoNow:  "刚刚",
	AgoMin:  "%d 分钟前",
	AgoHour: "%d 小时前",
	AgoDay:  "%d 天前",
}, words{
	TabGeneral: "General",
	TabMe:      "My Commits",
	TabBase:    "Branches",
	TabRepos:   "Repositories",
	TabIndex:   "Data",
	TabGlab:    "MR/PR",

	About:        "About",
	AppVersion:   "Sheaf %s",
	Lang:         "Language",
	CodeFont:     "Code font",
	CodeFontHint: "Code in diffs uses this font; Geist Mono if it's missing",
	LangHint:     "English unless the system is Chinese",
	LangAuto:     "System",
	LangZh:       "简体中文",
	LangEn:       "English",

	CLIHead:         "Command Line Tool",
	CLICommand:      "sheaf command",
	CLIHint:         "Query requirements from the terminal, with zsh completion",
	CLIInstall:      "Install",
	CLIUninstall:    "Uninstall",
	CLIInstalled:    "Installed at %s",
	CLINotInstalled: "Not installed",
	CLIForeign:      "%s is occupied by another file or link. Remove it first.",
	CLIFailed:       "Could not install or remove the command line tool: %v",

	Workspace: "Workspace",
	Loading:   "Loading…",

	OtherEmails:     "Other emails",
	OtherEmailsHint: "Addresses not in your git config",
	MeHead:          "My emails",
	FromGit:         "Automatic · from global git config",
	FromRepos:       "Automatic · from the config of %s",
	FromReposMore:   "Automatic · from the config of %s and %d more repos",
	Manual:          "Added by hand",
	AddEmail:        "+ Add email",
	EmailPh:         "name@example.com",
	Add:             "Add",
	EmailInvalid:    "That's not a valid email",
	EmailDup:        "Already added",
	RemoveEmail:     "Remove %s",
	EmailRemoved:    "Removed %s",
	MeHint:          "Commits from these emails count as \"me\".",
	ReqHead:         "Requirement number format",
	ReqPattern:      "Pattern",
	ReqPatternHint:  "A regex; leave empty for the default",
	ReqInvalid:      "Invalid regex; not saved",
	ReqHint:         "Match your team's ticket IDs, e.g. ABC-123.",

	GlabHead:    "GitLab (glab)",
	GlabCopied:  "Copied; paste it into Terminal",
	GlabDir:     "glab config directory",
	GlabDirHint: "Leave empty to use the system glab sign-in",
	GlabPh:      "Empty, or ~/glab/company-a",
	GlabMissing: "Folder not found; create it first (~ allowed)",
	GlabLogin:   "Sign in to this folder",
	GlabHint:    "Applies only to this workspace. Create the folder, run the command below, and replace <host> with your GitLab domain. GITLAB_TOKEN still works.",
	Copy:        "Copy",
	GhHead:      "GitHub (gh)",
	GhHint:      "Uses your gh sign-in to open PRs on GitHub.",
	GhLogin:     "Sign in to GitHub",
	GhCommand:   "gh auth login",
	GhCopy:      "Copy sign-in command",

	TargetsHint:        "“All changes” compares to it; progress follows the merge flow.",
	FlowHead:           "Merge flow",
	FlowHint:           "The branches changes merge into, in order. Empty uses the main branch; archive by hand when done.",
	FlowName:           "Stage %d name",
	FlowBranch:         "Stage %d branch",
	FlowAdd:            "+ Add stage",
	FlowUp:             "Move stage %d up",
	FlowDown:           "Move stage %d down",
	FlowRemove:         "Remove stage %d",
	FlowRemoved:        "Removed stage",
	FlowNameHead:       "Stage name",
	FlowBranchHead:     "Target branch",
	FlowBaseline:       " Final branch: %s.",
	BaselineOnly:       "Final branch only",
	BaselineOnlyRepo:   "%s final branch only",
	Master:             "Compare branch",
	CompareBranch:      "Branch",
	CompareBranchField: "Compare branch field",
	MasterHint:         "Changes, ↑↓ and MR/PRs use it",
	MasterPh:           "Each repo's main branch",
	BasesHead:          "Main branch of each repo",
	BaseMainDir:        "Main directory only",
	BaseDirs:           "%d more working directories",
	ColBase:            "Main branch",
	BaseAuto:           "auto",
	BaseHint:           "Choose a branch",
	BaseFoot:           "Auto tries origin/master, origin/main, master, main (%d repos). Repos missing a stage skip it.",
	NoRepos:            "No repositories found yet",
	ReposHead:          "Included repositories",
	ReposHint:          "%d repositories, %d excluded. Excluded ones don't appear in Requirements.",
	RepoTrack:          "Include %s",
	RepoInner:          "holds %d repos",
	RepoSearch:         "Search repositories",
	RepoCount:          "Included %d / %d",
	RepoOn:             "Included",
	RepoOff:            "Excluded",
	RepoNoMatch:        "No matching repositories",

	IndexLine:        "%s commits · updated %s",
	IndexNoTime:      "%s commits",
	IndexUpdating:    "%s commits · updating…",
	IndexProgress:    "Reading %d / %d",
	IndexError:       "Reading failed: %s",
	IndexFailed:      "%s failed to read: %s",
	IndexFailedRepos: "{n} repositories",
	IndexSub:         "%d repos, %d working dirs; updates on change",
	Refresh:          "Refresh",
	Refreshing:       "Refreshing…",
	Refreshed:        "Up to date",
	DataDir:          "Data location",
	Reveal:           "Show in Finder",
	CmdLog:           "Activity log",
	CmdLogSub:        "Every change made to your repos, kept 30 days",
	CmdLogOpen:       "Show activity log",
	IndexHead:        "Commit data",
	LocalData:        "Local data",
	IndexHint:        "Data stays on this Mac, never in your repositories.",

	AgoNow:  "just now",
	AgoMin:  "%d min ago",
	AgoHour: "%d h ago",
	AgoDay:  "%d days ago",
})

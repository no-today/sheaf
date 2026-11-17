# Sheaf 提交约定（给 Coding Agent）

[English](agent-commit-convention.md)

Sheaf 从**分支名**和**提交标题**里找需求号，把各仓库的改动归到同一个需求下；提交标题去掉编号后的部分，就是左栏显示的需求名。Agent 照下面的约定提交，Sheaf 就能把它写的改动认全。

把这句话发给你的 Coding Agent 就行：

> 读 https://github.com/no-today/sheaf/blob/main/docs/templates/agent-commit-convention.zh-CN.md ，按「配置到工作区」一节把提交约定装进这个工作区。

## 配置到工作区

写给 Agent，按顺序做：

1. **找到工作区根目录**：用户在 Sheaf 里打开的那个文件夹，下面放着各个仓库。不确定就问用户。
2. **确认需求号的样子**：Sheaf 默认认 `[A-Z][A-Z0-9]+-\d+` 这种写法（如 `PAY-142`、`AB2-7`）。问用户团队的编号长什么样；如果不是这种写法，提醒用户在 Sheaf 的**设置 → 我的提交 → 写法**里填上对应的正则，否则 Sheaf 认不出来。
3. **写进根目录的 Agent 指令文件**：把下面「约定正文」两个标记之间的内容（含标记）加到工作区根目录的 `CLAUDE.md`；用户用的是别的 Agent，就写进那个 Agent 读的文件（如 `AGENTS.md`）。文件不存在就新建。
   - 已经有 `<!-- sheaf:commit-convention -->` 标记：整段替换成新的，不要重复添加。
   - 用户的编号不是 `PAY-142` 这种样子：把正文里的例子换成团队真实的编号格式。
4. **不要擅自改各个仓库**：仓库里被 git 跟踪的指令文件改了就得提交，先问用户要不要每个仓库也加一份。
5. **告诉用户**改了哪些文件，第 2 步是否需要用户去 Sheaf 里改写法，以及想让 git 检查每次提交的话，可以装下面的「本机强制」。

## 本机强制（可选）

`githooks/dispatch` 检查工作区里的每次提交：标题开头没有编号，就按分支名补上；分支名里也没有，就拒绝提交。和分支名的编号不一致只提醒，不拦。合并、还原、fixup 不检查。它会先跑仓库自己的同名 hook（设了 `core.hooksPath` 后 git 不再跑它们）。团队编号不是 `PAY-142` 这种样子时，设环境变量 `SHEAF_REQUIREMENT_PATTERN`（扩展正则），和 Sheaf 里的写法一致。

它会改全局 git 配置，所以只在用户要求时由 Agent 安装：

```sh
WS=/path/to/workspace   # 在 Sheaf 里打开的那个文件夹
mkdir -p ~/.githooks
curl -fsSL https://raw.githubusercontent.com/no-today/sheaf/main/docs/templates/githooks/dispatch -o ~/.githooks/dispatch
chmod +x ~/.githooks/dispatch
(cd ~/.githooks && for h in applypatch-msg pre-applypatch post-applypatch pre-commit pre-merge-commit prepare-commit-msg commit-msg post-commit pre-rebase post-checkout post-merge pre-push post-rewrite pre-auto-gc push-to-checkout sendemail-validate; do ln -sf dispatch "$h"; done)
printf '[core]\n\thooksPath = ~/.githooks\n' > ~/.gitconfig-sheaf
git config --global includeIf."gitdir:$WS/".path ~/.gitconfig-sheaf
```

验证：在 `$WS` 下任一仓库运行 `git config core.hooksPath`，应输出 `~/.githooks`。临时跳过：`git commit --no-verify`。

## 约定正文

<!-- sheaf:commit-convention -->
## 提交约定

每个改动都属于一个需求，需求号形如 `PAY-142`。同一个需求在所有仓库用同一个号。

### 必须

- **分支名带完整需求号**：如 `feat-PAY-142`、`fix-PAY-142-timeout`。不要只写一部分（`PAY-14`），也不要把号拆开。
- **提交标题以需求号开头**，后面一个空格，再用一句话说清做了什么：`PAY-142 网关支持扣款`。
- **合并时保留 git 默认的合并信息**（`Merge branch 'feat-PAY-142' into ...`），不要改掉里面的分支名。squash 合并时，标题仍以需求号开头。
- **不知道需求号就先问**，不要自己编一个。确实不属于任何需求的小改动（改错别字、升级依赖），问用户能否不带编号。
- 不要在提交标题里写别的需求号；一个提交只属于一个需求。

### 建议

- 正文写**行为变化**，一条一行，用业务语言：谁、在什么情况下、变成什么样。纯重构写「行为不变」。不写「修改了 XxxService.java」。
- 下面几类改动各单独写一行：数据库脚本（改了哪些表、多少条）、权限（谁多了或少了什么）、开关和配置（名字、默认值）、改了布尔参数或参数顺序的调用（每个调用点和新值）。
- 一个提交只做一件事。

### 示例

```
PAY-142 网关支持扣款

下单后网关调用扣款接口，金额为 0 或负数时直接返回 0
DML：pay_record 新增唯一索引 uk_trade_no
```

```
PAY-142 从 Capture 抽出 captureOnce

行为不变
```
<!-- /sheaf:commit-convention -->

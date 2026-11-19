# 已知问题

暂不修的问题和没验收的地方。修好后删掉对应条目。

- **同时出现两条错误提示时，前一条被顶掉**：提示一次只显示一条（`widgets.toastID`），所以配置备份的提示可能被随后出现的“N 个仓库读取失败”盖掉。之后就再也看不到备份文件在哪，因为设置页不显示备份位置。
- **读取失败的仓在未提交页显示原始 git 报错**：HEAD 损坏的仓，每个文件行都直接显示整条 git 命令和 stderr（`git diff -M … : exit status 128: fatal: bad revision 'HEAD'`），这个仓还会显示为“无分支”，文件全标成“新文件”。
- **工作区根目录后来才变成仓库时，同名子仓的配置会转给根仓**：子仓 `api/` 先配好基线或忽略，之后再把根目录 `api` 本身 `git init`，子仓改名为 `./api`，原来按 `api` 存的配置落到根仓上。很少见，暂不处理。
- **启动时未提交数分批到达，页面每批都重算一次**：未提交数每 200 ms 至多推一批，打开时的两轮各推一遍；“未提交”页每批重载一次，需求列表每批重算一次（约 100 ms）。在预算内但偏重，可以只在这批改变了显示内容时才重算。
- **`TestDeletedRequirementWithoutOwnCommitsShowsUnconfirmed` 在 CI 偶发清理失败**：11-17 的 CI 报 `TempDir RemoveAll cleanup: … .git/objects: directory not empty`，本地连跑 30 次未复现。怀疑 track 页面自开的 goroutine 在调 git，测试清理时只取消 context、不等它们退出（`viewtest.Mount` 的 cleanup 只调 `Page.Close`），git 还在写仓库时临时目录已开始删除；但没找到会写对象的只读命令，未证实。再出现时先查清理时还有哪些 git 进程在跑。

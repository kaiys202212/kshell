# 冒烟：CodeBuddy 内置 + 自定义工具表单

分支：`feat/codebuddy-providers-form`

- [ ] 设置 → 工具检测列表出现 CodeBuddy；未装时有「安装」（`npm install -g @tencent-ai/codebuddy-code`），已装可卸载；可选清除 `~/.codebuddy`
- [ ] 安装/卸载成功后工具行刷新，无需重启
- [ ] 会话列表能扫到 `~/.codebuddy/projects/<ws>/<id>.jsonl`，不含 subagents 深层文件；恢复走 `--resume {id}`
- [ ] 自定义工具默认是表单：添加工具、填 ID、选择可执行文件/检测目录/会话目录后保存；成功文案为「已保存，已重新加载」，无「立即重启」
- [ ] 「源码」可编辑整份 yaml 并保存；切回表单字段同步（注释会丢）
- [ ] 保存后立即出现在工具检测与「选择 agent」（热重载）
- [ ] yaml 里残留 `id: codebuddy` 不会出现第二个 CodeBuddy，内置优先

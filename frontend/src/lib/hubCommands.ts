// 新命令别名识别：命中与否不影响旧命令路径（AgentHubPanel 先查旧表）。
// 这些命令语义在 Go 侧共享命令层实现（微信/桌面端同源，internal/hub/console.go），
// 前端只透传文本给 HubCommand 绑定，回复作为系统提示渲染进 hub 消息流。

// 与 Go 侧 handleCommand 的别名分支保持同步的新命令集合。
const NEW_COMMANDS = new Set(['?', 'l', 'la', 'en', 'n', 's', 'm', 'g', 'f', 'st', 'c']);

// isHubNumber 判断输入是否为纯数字（正整数）——命令层的「编号切换/菜单选择」输入。
// 与 Go 侧 strconv.Atoi + n > 0 对齐：前导 0 的数字串（如 '03'）按字面匹配也成立；
// 负数、非数字、空串、含空白（未 trim）均不成立。
export function isHubNumber(text: string): boolean {
  if (!/^\d+$/.test(text)) return false;
  // 逐位数字但全为 0（'0'、'000'）在命令层不是有效编号，按非编号处理。
  return parseInt(text, 10) > 0;
}

// isNewHubCommand 判断输入是否为新命令别名：trim 后以 # 开头，且首词
// （去 #、小写）在 NEW_COMMANDS 集合内。#g/#f 等带参数形态只查首词，
// 参数由调用方连同完整原文透传给命令层。
export function isNewHubCommand(input: string): boolean {
  const text = input.trim();
  if (!text.startsWith('#')) return false;
  const first = (text.split(/\s+/)[0] ?? '').slice(1).toLowerCase();
  return NEW_COMMANDS.has(first);
}

// 新命令别名识别：命中与否不影响旧命令路径（AgentHubPanel 先查旧表）。
// 这些命令语义在 Go 侧共享命令层实现（微信/桌面端同源，internal/hub/console.go），
// 前端只透传文本给 HubCommand 绑定，回复作为系统提示渲染进 hub 消息流。

// 与 Go 侧 handleCommand 的别名分支保持同步的新命令集合。
const NEW_COMMANDS = new Set(['?', 'l', 'la', 'en', 'n', 's', 'm', 'g', 'f', 'st', 'c', 'h']);

// normalizeHubProbe 命令探测前的全角归一化（与 Go 侧 normalizeInput 同口径）：
// ＃→#、全角空格/冒号→半角空格、全角数字→半角。仅用于识别判断，不改写发给
// 命令层/agent 的原文（Go 侧命令层有自己的归一化，两端行为一致）。
export function normalizeHubProbe(text: string): string {
  return text
    .replace(/＃/g, '#')
    .replace(/[　：]/g, ' ')
    .replace(/[０-９]/g, (ch) => String.fromCharCode(ch.charCodeAt(0) - 0xff10 + 0x30));
}

// isHubNumber 判断输入是否为纯数字（正整数）——命令层的「编号切换/菜单选择」输入。
// 与 Go 侧 strconv.Atoi + n > 0 对齐：前导 0 的数字串（如 '03'）按字面匹配也成立；
// 负数、非数字、空串、含空白（未 trim）均不成立。全角数字归一化后判定。
export function isHubNumber(text: string): boolean {
  const norm = normalizeHubProbe(text);
  if (!/^\d+$/.test(norm)) return false;
  // 逐位数字但全为 0（'0'、'000'）在命令层不是有效编号，按非编号处理。
  return parseInt(norm, 10) > 0;
}

// isNewHubCommand 判断输入是否为新命令别名：trim 后以 #（含全角＃）开头，且首词
// （去 #、小写）在 NEW_COMMANDS 集合内。#g/#f 等带参数形态只查首词，
// 参数由调用方连同完整原文透传给命令层。
export function isNewHubCommand(input: string): boolean {
  const text = normalizeHubProbe(input.trim());
  if (!text.startsWith('#')) return false;
  const first = (text.split(/\s+/)[0] ?? '').slice(1).toLowerCase();
  return NEW_COMMANDS.has(first);
}

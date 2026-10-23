// 会话创建入口的「工具可用」统一判定（通用规则，2026-10-23 用户确认）：
// 探测结论为确定未登录（Auth === 'out'）的工具在所有新建会话入口不可见——
// 未登录时新建 ACP 会被 agent 拒绝（authRequired），kshell 侧卡在等待登录态；
// 登录入口收敛到设置页「工具」分区（徽标 + 登录按钮 + 重新扫描）。
// Auth === 'unknown'（探测不稳定）保守放行：宁可多跑一次失败的会话，不错杀已登录用户。
import type { ToolInfo } from './api';

// usableForSessionMenu：工作区「新建会话」下拉口径——已安装且有可执行文件，
// 且不是确定未登录。
export function usableForSessionMenu(t: ToolInfo): boolean {
  return !!t.Installed && t.BinPath !== '' && t.Auth !== 'out';
}

// usableForACP：AgentHub（新建弹窗 / 默认设置 / easynew）与需求派发口径——
// ACP 可用且不是确定未登录。
export function usableForACP(t: ToolInfo): boolean {
  return t.ACP?.Available === true && t.Auth !== 'out';
}

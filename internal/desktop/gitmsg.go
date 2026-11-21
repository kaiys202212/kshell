package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/commitmsg"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/workspace"
)

// gitMsgTimeout 覆盖一次 LLM 生成提交信息；hubtools 客户端自身 60s 超时，
// 这里略短以便先于客户端退出并给出可翻译错误。
const gitMsgTimeout = 45 * time.Second

var (
	errCommitMsgModelNotReady = errors.New("err.git.ai_model_not_ready")
	errCommitMsgNoStaged      = errors.New("err.git.ai_no_staged")
	errCommitMsgEmpty         = errors.New("err.git.ai_empty")
)

// GenerateCommitMessage 汇总已暂存变更，调用平台默认模型生成单行提交信息。
// 本地与 SSH 远端工作区走各自仓库实现；lang 以 zh 开头时要求中文输出。
func (a *App) GenerateCommitMessage(wsPath, repoRel, lang string) (string, error) {
	cfg, ok := hubtools.FromModelConfig(a.snapshot().Config.Model)
	if !ok {
		return "", errCommitMsgModelNotReady
	}
	// opencode GO 网关要求稳定会话头，缺失直接 400（见 llm_session.go）。
	cfg.SessionID = a.llmSessionID()

	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return "", err
	}
	var diff workspace.DiffResult
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		diff, err = g.stagedDiff(ctx, repoRel)
	} else {
		diff, err = workspace.StagedDiff(local, repoRel)
	}
	if err != nil {
		return "", fmt.Errorf("err.git.ai_diff_failed|%s", oneLine(err.Error()))
	}
	if strings.TrimSpace(diff.Text) == "" {
		return "", errCommitMsgNoStaged
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitMsgTimeout)
	defer cancel()
	msgs := commitmsg.BuildMessages(lang, commitmsg.TruncateDiff(diff.Text, commitmsg.MaxDiffBytes))
	resp, err := hubtools.NewClient(cfg).Chat(ctx, msgs)
	if err != nil {
		return "", fmt.Errorf("err.git.ai_failed|%s", oneLine(err.Error()))
	}
	msg := commitmsg.Sanitize(resp.Text)
	if msg == "" {
		return "", errCommitMsgEmpty
	}
	return msg, nil
}

// oneLine 把底层错误压成单行并去掉 `|`（wire 错误报文的分隔符），
// 避免 translateBackend 把错误细节误当参数拆开。
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "|", " ")
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > 200 {
		s = string(runes[:200])
	}
	return s
}

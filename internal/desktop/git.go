package desktop

import (
	"errors"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/workspace"
)

// 远端 git 完整实现见 Task6；此处先拒绝，避免 ssh Ref 被 filepath.Clean 弄坏。
var errRemoteGitUnavailable = errors.New("err.remote.git_unavailable")

func (a *App) gitLocalRoot(wsPath string) (string, error) {
	kind, local, _, err := a.parseWSRef(wsPath)
	if err != nil {
		return "", err
	}
	if kind == discovery.KindSSH {
		return "", errRemoteGitUnavailable
	}
	return local, nil
}

func (a *App) GitSCM(wsPath, repoRel, syncRemote string) (workspace.SCMSnapshot, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.SCMSnapshot{}, err
	}
	return workspace.SCMStatus(root, repoRel, syncRemote)
}

func (a *App) GitDiff(wsPath, repoRel, path, side string) (workspace.DiffResult, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.DiffResult{}, err
	}
	return workspace.FileDiff(root, repoRel, path, side)
}

func (a *App) GitStage(wsPath, repoRel string, paths []string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Stage(root, repoRel, paths)
}

func (a *App) GitUnstage(wsPath, repoRel string, paths []string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Unstage(root, repoRel, paths)
}

func (a *App) GitDiscard(wsPath, repoRel string, paths []string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Discard(root, repoRel, paths)
}

func (a *App) GitCommit(wsPath, repoRel, message string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Commit(root, repoRel, message)
}

func (a *App) GitStageHunk(wsPath, repoRel, path, side, patch string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.ApplyHunk(root, repoRel, path, side, patch, "stage")
}

func (a *App) GitUnstageHunk(wsPath, repoRel, path, side, patch string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.ApplyHunk(root, repoRel, path, side, patch, "unstage")
}

func (a *App) GitDiscardHunk(wsPath, repoRel, path, side, patch string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.ApplyHunk(root, repoRel, path, side, patch, "discard")
}

func (a *App) GitBranches(wsPath, repoRel string) ([]string, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return nil, err
	}
	return workspace.Branches(root, repoRel)
}

func (a *App) GitCheckout(wsPath, repoRel, name string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Checkout(root, repoRel, name)
}

func (a *App) GitCreateBranch(wsPath, repoRel, name string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.CreateBranch(root, repoRel, name)
}

func (a *App) GitFetch(wsPath, repoRel, remote string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Fetch(root, repoRel, remote)
}

func (a *App) GitPull(wsPath, repoRel, remote string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Pull(root, repoRel, remote)
}

func (a *App) GitPush(wsPath, repoRel, remote string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.Push(root, repoRel, remote)
}

func (a *App) GitStashPush(wsPath, repoRel, message string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.StashPush(root, repoRel, message)
}

func (a *App) GitStashPop(wsPath, repoRel string, index int) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.StashPop(root, repoRel, index)
}

func (a *App) GitStashApply(wsPath, repoRel string, index int) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.StashApply(root, repoRel, index)
}

func (a *App) GitStashDrop(wsPath, repoRel string, index int) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.StashDrop(root, repoRel, index)
}

func (a *App) GitLog(wsPath, repoRel, mode, ref string, limit, skip int) ([]workspace.LogCommit, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return nil, err
	}
	return workspace.Log(root, repoRel, mode, ref, limit, skip)
}

func (a *App) GitRefs(wsPath, repoRel string) ([]workspace.GitRef, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return nil, err
	}
	return workspace.Refs(root, repoRel)
}

func (a *App) GitFetchAll(wsPath, repoRel string) error {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	return workspace.FetchAll(root, repoRel)
}

func (a *App) GitCommitStat(wsPath, repoRel, hash string) (workspace.CommitStat, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.CommitStat{}, err
	}
	return workspace.CommitStatAt(root, repoRel, hash)
}

func (a *App) GitCommitDiff(wsPath, repoRel, hash string) (workspace.DiffResult, error) {
	root, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.DiffResult{}, err
	}
	return workspace.CommitDiff(root, repoRel, hash)
}

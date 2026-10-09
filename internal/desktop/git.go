package desktop

import (
	"errors"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/workspace"
)

// 远端无 git / 探测失败时的稳定 wire key；前端经 translateBackend 展示降级。
var errRemoteGitUnavailable = errors.New("err.remote.git_unavailable")

func (a *App) gitLocalRoot(wsPath string) (string, *remoteWS, error) {
	kind, local, remote, err := a.parseWSRef(wsPath)
	if err != nil {
		return "", nil, err
	}
	if kind == discovery.KindSSH {
		return "", remote, nil
	}
	return local, nil, nil
}

func (a *App) GitSCM(wsPath, repoRel, syncRemote string) (workspace.SCMSnapshot, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.SCMSnapshot{}, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.scm(ctx, repoRel, syncRemote)
	}
	return workspace.SCMStatus(local, repoRel, syncRemote)
}

func (a *App) GitDiff(wsPath, repoRel, path, side string) (workspace.DiffResult, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.DiffResult{}, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.fileDiff(ctx, repoRel, path, side)
	}
	return workspace.FileDiff(local, repoRel, path, side)
}

func (a *App) GitStage(wsPath, repoRel string, paths []string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.stage(ctx, repoRel, paths)
	}
	return workspace.Stage(local, repoRel, paths)
}

func (a *App) GitUnstage(wsPath, repoRel string, paths []string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.unstage(ctx, repoRel, paths)
	}
	return workspace.Unstage(local, repoRel, paths)
}

func (a *App) GitDiscard(wsPath, repoRel string, paths []string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.discard(ctx, repoRel, paths)
	}
	return workspace.Discard(local, repoRel, paths)
}

func (a *App) GitCommit(wsPath, repoRel, message string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.commit(ctx, repoRel, message)
	}
	return workspace.Commit(local, repoRel, message)
}

func (a *App) GitStageHunk(wsPath, repoRel, path, side, patch string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.applyHunk(ctx, repoRel, path, side, patch, "stage")
	}
	return workspace.ApplyHunk(local, repoRel, path, side, patch, "stage")
}

func (a *App) GitUnstageHunk(wsPath, repoRel, path, side, patch string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.applyHunk(ctx, repoRel, path, side, patch, "unstage")
	}
	return workspace.ApplyHunk(local, repoRel, path, side, patch, "unstage")
}

func (a *App) GitDiscardHunk(wsPath, repoRel, path, side, patch string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.applyHunk(ctx, repoRel, path, side, patch, "discard")
	}
	return workspace.ApplyHunk(local, repoRel, path, side, patch, "discard")
}

func (a *App) GitBranches(wsPath, repoRel string) ([]string, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return nil, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.branches(ctx, repoRel)
	}
	return workspace.Branches(local, repoRel)
}

func (a *App) GitCheckout(wsPath, repoRel, name string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.checkout(ctx, repoRel, name)
	}
	return workspace.Checkout(local, repoRel, name)
}

func (a *App) GitCreateBranch(wsPath, repoRel, name string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.createBranch(ctx, repoRel, name)
	}
	return workspace.CreateBranch(local, repoRel, name)
}

func (a *App) GitFetch(wsPath, repoRel, remoteName string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitRemoteTimeout)
		defer cancel()
		return g.fetch(ctx, repoRel, remoteName)
	}
	return workspace.Fetch(local, repoRel, remoteName)
}

func (a *App) GitPull(wsPath, repoRel, remoteName string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitRemoteTimeout)
		defer cancel()
		return g.pull(ctx, repoRel, remoteName)
	}
	return workspace.Pull(local, repoRel, remoteName)
}

func (a *App) GitPush(wsPath, repoRel, remoteName string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitRemoteTimeout)
		defer cancel()
		return g.push(ctx, repoRel, remoteName)
	}
	return workspace.Push(local, repoRel, remoteName)
}

func (a *App) GitStashPush(wsPath, repoRel, message string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.stashPush(ctx, repoRel, message)
	}
	return workspace.StashPush(local, repoRel, message)
}

func (a *App) GitStashPop(wsPath, repoRel string, index int) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.stashPop(ctx, repoRel, index)
	}
	return workspace.StashPop(local, repoRel, index)
}

func (a *App) GitStashApply(wsPath, repoRel string, index int) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.stashApply(ctx, repoRel, index)
	}
	return workspace.StashApply(local, repoRel, index)
}

func (a *App) GitStashDrop(wsPath, repoRel string, index int) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.stashDrop(ctx, repoRel, index)
	}
	return workspace.StashDrop(local, repoRel, index)
}

func (a *App) GitLog(wsPath, repoRel, mode, ref string, limit, skip int) ([]workspace.LogCommit, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return nil, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.log(ctx, repoRel, mode, ref, limit, skip)
	}
	return workspace.Log(local, repoRel, mode, ref, limit, skip)
}

func (a *App) GitRefs(wsPath, repoRel string) ([]workspace.GitRef, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return nil, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.refs(ctx, repoRel)
	}
	return workspace.Refs(local, repoRel)
}

func (a *App) GitFetchAll(wsPath, repoRel string) error {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitRemoteTimeout)
		defer cancel()
		return g.fetchAll(ctx, repoRel)
	}
	return workspace.FetchAll(local, repoRel)
}

func (a *App) GitCommitStat(wsPath, repoRel, hash string) (workspace.CommitStat, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.CommitStat{}, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.commitStat(ctx, repoRel, hash)
	}
	return workspace.CommitStatAt(local, repoRel, hash)
}

func (a *App) GitCommitDiff(wsPath, repoRel, hash string) (workspace.DiffResult, error) {
	local, remote, err := a.gitLocalRoot(wsPath)
	if err != nil {
		return workspace.DiffResult{}, err
	}
	if remote != nil {
		g := a.remoteGit(remote)
		ctx, cancel := g.withTimeout(remoteGitStatusTimeout)
		defer cancel()
		return g.commitDiff(ctx, repoRel, hash)
	}
	return workspace.CommitDiff(local, repoRel, hash)
}

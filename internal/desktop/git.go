package desktop

import (
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/workspace"
)

func (a *App) GitSCM(wsPath, repoRel string) (workspace.SCMSnapshot, error) {
	return workspace.SCMStatus(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

func (a *App) GitDiff(wsPath, repoRel, path, side string) (workspace.DiffResult, error) {
	return workspace.FileDiff(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, path, side)
}

func (a *App) GitStage(wsPath, repoRel string, paths []string) error {
	return workspace.Stage(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, paths)
}

func (a *App) GitUnstage(wsPath, repoRel string, paths []string) error {
	return workspace.Unstage(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, paths)
}

func (a *App) GitDiscard(wsPath, repoRel string, paths []string) error {
	return workspace.Discard(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, paths)
}

func (a *App) GitCommit(wsPath, repoRel, message string) error {
	return workspace.Commit(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, message)
}

func (a *App) GitStageHunk(wsPath, repoRel, path, side, patch string) error {
	return workspace.ApplyHunk(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, path, side, patch, "stage")
}

func (a *App) GitUnstageHunk(wsPath, repoRel, path, side, patch string) error {
	return workspace.ApplyHunk(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, path, side, patch, "unstage")
}

func (a *App) GitDiscardHunk(wsPath, repoRel, path, side, patch string) error {
	return workspace.ApplyHunk(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, path, side, patch, "discard")
}

func (a *App) GitBranches(wsPath, repoRel string) ([]string, error) {
	return workspace.Branches(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

func (a *App) GitCheckout(wsPath, repoRel, name string) error {
	return workspace.Checkout(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, name)
}

func (a *App) GitCreateBranch(wsPath, repoRel, name string) error {
	return workspace.CreateBranch(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, name)
}

func (a *App) GitFetch(wsPath, repoRel string) error {
	return workspace.Fetch(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

func (a *App) GitPull(wsPath, repoRel string) error {
	return workspace.Pull(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

func (a *App) GitPush(wsPath, repoRel string) error {
	return workspace.Push(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

func (a *App) GitStashPush(wsPath, repoRel, message string) error {
	return workspace.StashPush(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, message)
}

func (a *App) GitStashPop(wsPath, repoRel string, index int) error {
	return workspace.StashPop(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, index)
}

func (a *App) GitStashApply(wsPath, repoRel string, index int) error {
	return workspace.StashApply(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, index)
}

func (a *App) GitStashDrop(wsPath, repoRel string, index int) error {
	return workspace.StashDrop(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, index)
}

func (a *App) GitLog(wsPath, repoRel, mode, ref string, limit int) ([]workspace.LogCommit, error) {
	return workspace.Log(filepath.Clean(strings.TrimSpace(wsPath)), repoRel, mode, ref, limit)
}

func (a *App) GitRefs(wsPath, repoRel string) ([]workspace.GitRef, error) {
	return workspace.Refs(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

func (a *App) GitFetchAll(wsPath, repoRel string) error {
	return workspace.FetchAll(filepath.Clean(strings.TrimSpace(wsPath)), repoRel)
}

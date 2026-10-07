package workspace

import (
	"context"
	"strings"
)

// CommitDiff 取某次提交的完整 patch（含根提交；无 commit message）。
func CommitDiff(wsRoot, repoRel, hash string) (DiffResult, error) {
	if err := validCommitHash(hash); err != nil {
		return DiffResult{}, err
	}
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return DiffResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, "show", "--format=", "--patch", strings.TrimSpace(hash)).CombinedOutput()
	if err != nil && len(out) == 0 {
		return DiffResult{}, gitErr(out, err)
	}
	text := string(out)
	binary := strings.Contains(text, "Binary files ") || strings.Contains(text, "GIT binary patch")
	return DiffResult{Text: text, Binary: binary}, nil
}

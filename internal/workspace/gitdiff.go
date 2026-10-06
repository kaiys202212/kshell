package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	errBadPatch  = errors.New("hunk 缺少 diff --git 头")
	errBadDiffOp = errors.New("无效 hunk 操作")
	errBadSide   = errors.New("无效 diff 侧")
)

// DiffResult 某文件相对所选仓库的 unified diff。
type DiffResult struct {
	Text      string
	Binary    bool
	Untracked bool
}

// FileDiff 取 working 或 staged 的 unified diff。
func FileDiff(wsRoot, repoRel, path, side string) (DiffResult, error) {
	if side != "working" && side != "staged" {
		return DiffResult{}, errBadSide
	}
	abs, rels, err := repoPathArgs(wsRoot, repoRel, []string{path})
	if err != nil {
		return DiffResult{}, err
	}
	rel := rels[0]
	tracked := gitRunAt(abs, gitStatusTimeout, "ls-files", "--error-unmatch", "--", rel) == nil
	if !tracked {
		if side == "staged" {
			return DiffResult{}, nil
		}
		return untrackedDiff(abs, rel)
	}
	args := []string{"diff", "--"}
	if side == "staged" {
		args = []string{"diff", "--cached", "--"}
	}
	args = append(args, rel)
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, abs, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return DiffResult{}, gitErr(out, err)
	}
	text := string(out)
	binary := strings.Contains(text, "Binary files ") || strings.Contains(text, "GIT binary patch")
	return DiffResult{Text: text, Binary: binary}, nil
}

func untrackedDiff(root, rel string) (DiffResult, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	b, err := os.ReadFile(full)
	if err != nil {
		return DiffResult{}, err
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return DiffResult{Binary: true, Untracked: true}, nil
	}
	norm := strings.ReplaceAll(string(b), "\r\n", "\n")
	norm = strings.TrimSuffix(norm, "\n")
	var body strings.Builder
	n := 0
	if norm == "" && len(b) == 0 {
		n = 0
	} else {
		for _, line := range strings.Split(norm, "\n") {
			n++
			body.WriteString("+")
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	hunk := "@@ -0,0 +0,0 @@\n"
	if n > 0 {
		hunk = fmt.Sprintf("@@ -0,0 +1,%d @@\n", n)
	}
	text := fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n%s%s",
		rel, rel, rel, hunk, body.String())
	return DiffResult{Text: text, Untracked: true}, nil
}

// ApplyHunk 对完整 unified patch 执行 stage / unstage / discard。
func ApplyHunk(wsRoot, repoRel, path, side, hunkPatch, op string) error {
	if _, _, err := repoPathArgs(wsRoot, repoRel, []string{path}); err != nil {
		return err
	}
	if !strings.Contains(hunkPatch, "diff --git") {
		return errBadPatch
	}
	abs, err := ResolveRepo(wsRoot, repoRel)
	if err != nil {
		return err
	}
	args := []string{"apply", "--whitespace=nowarn"}
	switch op {
	case "stage":
		args = append(args, "--cached")
	case "unstage":
		args = append(args, "-R", "--cached")
	case "discard":
		args = append(args, "-R")
	default:
		return errBadDiffOp
	}
	_ = side
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	cmd := gitCmd(ctx, abs, args...)
	cmd.Stdin = strings.NewReader(hunkPatch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return gitErr(out, err)
	}
	return nil
}

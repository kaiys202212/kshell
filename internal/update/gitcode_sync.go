package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncOptions 把 dist 里的 zip 与 SHA256SUMS 同步到 GitCode Release。
type SyncOptions struct {
	BaseURL string
	Token   string
	Owner   string
	Repo    string
	Tag     string
	Dir     string
	HTTP    *http.Client
}

// SyncGitCode 无 Token 时直接跳过；有 Token 则创建/复用 Release 并 PUT 附件。
func SyncGitCode(ctx context.Context, opts SyncOptions) error {
	if strings.TrimSpace(opts.Token) == "" {
		return nil
	}
	if strings.TrimSpace(opts.Tag) == "" {
		return fmt.Errorf("缺少 tag")
	}
	if opts.Dir == "" {
		return fmt.Errorf("缺少产物目录")
	}
	owner := orDefault(opts.Owner, GitCodeOwner)
	repo := orDefault(opts.Repo, GitCodeRepo)
	base := strings.TrimRight(orDefault(opts.BaseURL, "https://api.gitcode.com/api/v5"), "/")
	cli := opts.HTTP
	if cli == nil {
		// 单个 zip ~8MB，GitCode 对象存储偶发超过 5 分钟
		cli = &http.Client{Timeout: 15 * time.Minute}
	}

	files, err := listReleaseFiles(opts.Dir)
	if err != nil {
		return err
	}
	api := gitcodeAPI{base: base, token: opts.Token, owner: owner, repo: repo, http: cli}

	exists, err := api.releaseExists(ctx, opts.Tag)
	if err != nil {
		return err
	}
	if !exists {
		if err := api.createRelease(ctx, opts.Tag); err != nil {
			return err
		}
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if err := api.upload(ctx, opts.Tag, filepath.Base(f), body); err != nil {
			return fmt.Errorf("上传 %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

func orDefault(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func listReleaseFiles(dir string) ([]string, error) {
	zips, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		return nil, err
	}
	// DMG 安装包与 zip 一起作为发布附件同步（zip 供应用内自更新，DMG 供人工安装）
	dmgs, err := filepath.Glob(filepath.Join(dir, "*.dmg"))
	if err != nil {
		return nil, err
	}
	sums := filepath.Join(dir, SumsName)
	out := append(append([]string{}, zips...), dmgs...)
	if _, err := os.Stat(sums); err == nil {
		out = append(out, sums)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 没有 zip/dmg 或 SHA256SUMS", dir)
	}
	return out, nil
}

type gitcodeAPI struct {
	base, token, owner, repo string
	http                     *http.Client
}

func (a gitcodeAPI) releaseExists(ctx context.Context, tag string) (bool, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/repos/"+a.owner+"/"+a.repo+"/releases/tags/"+tag, nil)
	if err != nil {
		return false, err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("查询 GitCode Release: HTTP %d", resp.StatusCode)
	}
	return true, nil
}

func (a gitcodeAPI) createRelease(ctx context.Context, tag string) error {
	payload, _ := json.Marshal(map[string]string{
		"tag_name":       tag,
		"name":           tag,
		"body":           "kshell " + tag,
		"release_status": "latest",
	})
	req, err := a.newReq(ctx, http.MethodPost, "/repos/"+a.owner+"/"+a.repo+"/releases", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnprocessableEntity {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("创建 GitCode Release: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (a gitcodeAPI) upload(ctx context.Context, tag, fileName string, body []byte) error {
	const attempts = 3
	var last error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * 2 * time.Second):
			}
		}
		last = a.uploadOnce(ctx, tag, fileName, body)
		if last == nil {
			return nil
		}
	}
	return last
}

func (a gitcodeAPI) uploadOnce(ctx context.Context, tag, fileName string, body []byte) error {
	q := url.Values{"file_name": {fileName}}
	req, err := a.newReq(ctx, http.MethodGet, "/repos/"+a.owner+"/"+a.repo+"/releases/"+tag+"/upload_url?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("获取上传地址: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var info struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return fmt.Errorf("解析上传地址: %w", err)
	}
	if info.URL == "" {
		return fmt.Errorf("上传地址为空")
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, info.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range info.Headers {
		put.Header.Set(k, v)
	}
	putResp, err := a.http.Do(put)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	_, _ = io.Copy(io.Discard, putResp.Body)
	if putResp.StatusCode < 200 || putResp.StatusCode >= 300 {
		return fmt.Errorf("PUT 附件: HTTP %d", putResp.StatusCode)
	}
	return nil
}

func (a gitcodeAPI) newReq(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u := a.base + path
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("User-Agent", userAgent)
	q := req.URL.Query()
	q.Set("access_token", a.token)
	req.URL.RawQuery = q.Encode()
	return req, nil
}

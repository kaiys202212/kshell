package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://skills.sh"

// Client 对接 skills.sh 搜索与下载 API。
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) base() string {
	if c != nil && c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Search 按关键词搜索 skill；q 去空格后长度须 ≥2。
func (c *Client) Search(ctx context.Context, q string, limit int) ([]Summary, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return nil, errQueryTooShort
	}
	if limit <= 0 {
		limit = 20
	}
	u, err := url.Parse(c.base() + "/api/search")
	if err != nil {
		return nil, fmt.Errorf("%w|%v", errSearchFailed, err)
	}
	qs := u.Query()
	qs.Set("q", q)
	qs.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = qs.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w|%v", errSearchFailed, err)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w|%v", errSearchFailed, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w|http %d", errSearchFailed, res.StatusCode)
	}
	var parsed struct {
		Skills []Summary `json:"skills"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w|%v", errSearchFailed, err)
	}
	if parsed.Skills == nil {
		return []Summary{}, nil
	}
	return parsed.Skills, nil
}

// Download 按 id（owner/repo/skillId）下载 skill 文件集合。
func (c *Client) Download(ctx context.Context, id string) ([]File, error) {
	owner, repo, skillID, err := splitID(id)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/api/download/%s/%s/%s", c.base(),
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(skillID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w|%v", errDownloadFail, err)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w|%v", errDownloadFail, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w|http %d", errDownloadFail, res.StatusCode)
	}
	var parsed struct {
		Files []File `json:"files"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w|%v", errDownloadFail, err)
	}
	if len(parsed.Files) == 0 {
		return nil, errInvalidSkill
	}
	return parsed.Files, nil
}

// splitID 解析 owner/repo/skillId；repo 段允许含斜杠以外的多段时取中间全部。
func splitID(id string) (owner, repo, skillID string, err error) {
	id = strings.TrimSpace(id)
	parts := strings.Split(id, "/")
	if len(parts) < 3 {
		return "", "", "", errInvalidID
	}
	owner = parts[0]
	skillID = parts[len(parts)-1]
	repo = strings.Join(parts[1:len(parts)-1], "/")
	if owner == "" || repo == "" || skillID == "" {
		return "", "", "", errInvalidID
	}
	return owner, repo, skillID, nil
}

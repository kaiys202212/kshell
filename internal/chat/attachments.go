package chat

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

// mimeExtByPrefix 是 MimeType → 扩展名映射；未列出的走 .bin 兜底。
var mimeExtByPrefix = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
	"image/bmp":  ".bmp",
	"image/svg":  ".svg",
}

// mimeExt 把 MimeType 转成落盘扩展名；未知或空返回 .bin（表驱动，便于扩充）。
func mimeExt(mime string) string {
	if ext, ok := mimeExtByPrefix[mime]; ok {
		return ext
	}
	return ".bin"
}

// persistBlocks 遍历用户消息内容块，把 image / 内嵌 resource 的 base64 解码落盘
// 到 root/<chatID>/ 下，返回逐块对应的 AttachmentMeta（前端据此渲染与点击预览）。
// 规则：
//   - text 块跳过（文本走时间线 Text 字段）；
//   - image/resource 落盘，meta 带 Path；解码或写失败时该块 meta 不带 Path（不 panic）；
//   - resource_link 只保留 URI，不落盘；
//   - audio 保持旧兼容行为：meta 仅 MimeType（Kind 留空）；
//   - root 为空串表示未配置缓存目录：完全不落盘，image/resource 的 meta 不带 Path。
//
// 单个块的失败只降级该块，不影响其余块与消息投递本身。
func persistBlocks(root, chatID string, blocks []acp.ContentBlock) []AttachmentMeta {
	var out []AttachmentMeta
	if root == "" || len(blocks) == 0 {
		// 未配置落盘：仍生成元数据，行为与旧 attachmentMetas 兼容。
		for _, b := range blocks {
			if b.Type == "text" {
				continue
			}
			out = append(out, metaWithoutPersist(b))
		}
		return out
	}
	dir := filepath.Join(root, chatID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// 目录建不出来（权限/磁盘）：全部降级为不带 Path 的元数据。
		for _, b := range blocks {
			out = append(out, metaWithoutPersist(b))
		}
		return out
	}
	for i, b := range blocks {
		switch b.Type {
		case "text":
			continue
		case "image":
			meta := AttachmentMeta{Kind: "image", MimeType: b.MimeType}
			meta.Path = persistBase64(dir, strconv.FormatInt(time.Now().UnixNano(), 36)+"-"+strconv.Itoa(i)+mimeExt(b.MimeType), b.Data)
			out = append(out, meta)
		case "resource":
			meta := AttachmentMeta{Kind: "file", Name: b.Name, MimeType: b.MimeType}
			// ACP 规范把内嵌资源内容放在 resource.blob；部分实现直接用顶层 data，两者都接。
			data, mime := b.Data, b.MimeType
			if data == "" && b.Resource != nil {
				data = b.Resource.Blob
			}
			if mime == "" && b.Resource != nil {
				mime = b.Resource.MimeType
			}
			meta.MimeType = mime
			meta.Path = persistBase64(dir, strconv.FormatInt(time.Now().UnixNano(), 36)+"-"+strconv.Itoa(i)+mimeExt(mime), data)
			out = append(out, meta)
		case "resource_link":
			out = append(out, AttachmentMeta{Kind: "file", Name: b.Name, MimeType: b.MimeType, URI: b.URI})
		case "audio":
			// 音频暂无预览诉求，维持旧形态（Kind 留空按 file 兜底渲染）。
			out = append(out, AttachmentMeta{MimeType: b.MimeType})
		default:
			out = append(out, AttachmentMeta{Name: b.Name, MimeType: b.MimeType})
		}
	}
	return out
}

// persistBase64 解码 data 并写到 dir/name；失败返回空串（meta 降级不带 Path）。
func persistBase64(dir, name, data string) string {
	if data == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return ""
	}
	return path
}

// metaWithoutPersist 是未落盘路径下的元数据兜底：Kind 照常标注（前端可区分
// 图片/文件），仅 Path 缺席；audio 维持旧形态（Kind 留空）。
func metaWithoutPersist(b acp.ContentBlock) AttachmentMeta {
	switch b.Type {
	case "text":
		return AttachmentMeta{}
	case "image":
		return AttachmentMeta{Kind: "image", MimeType: b.MimeType}
	case "audio":
		return AttachmentMeta{MimeType: b.MimeType}
	case "resource_link":
		return AttachmentMeta{Kind: "file", Name: b.Name, MimeType: b.MimeType, URI: b.URI}
	default:
		return AttachmentMeta{Kind: "file", Name: b.Name, MimeType: b.MimeType}
	}
}

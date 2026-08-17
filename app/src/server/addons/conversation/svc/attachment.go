// Package svc 的附件处理：校验 → 落 message metadata → dispatch 时签 URL。
//
// 三个环节刻意分离，因为信任级别与时效性不同：
//   - 校验（resolveAttachments）：fileId 来自前端，必须向 storage 核对；
//   - 持久化（attachmentsToMetadata）：只存稳定字段，**不存签名 URL**；
//   - 签发（signAttachments）：URL 有效期有限（storage 侧 1800s），每次 dispatch
//     现签现用。若把 URL 写进 DB，几小时后历史消息里全是死链。
package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nucleagent/nucleagent-shared/a2a"
	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"
	"github.com/kwhitestone/prism-fusion/global"

	"nucleagent-core/internal/storageclient"
)

// ErrInvalidAttachment 附件不可用（客户端问题：引用了不存在/未完成/越权的文件）。
//
// 存在的意义是让 router 能把它映射成 4xx。若一律按 500 返回，用户看到的是
// "服务器故障"而不是"这个附件有问题，重新上传"，而后者才是可自行修正的。
var ErrInvalidAttachment = errors.New("附件不可用")

// warnAttach 记一条附件相关的告警。
//
// 包一层是因为 global.PRISM_LOG 只在框架启动后才被赋值：附件失败是**错误路径**，
// 若这条路径本身因 logger 为 nil 而 panic，就会把「一个附件有问题」升级成
// 「整个请求崩掉」—— 正好是这段代码想避免的。单测里也不必造 logger。
func warnAttach(msg string, fields ...zap.Field) {
	if global.PRISM_LOG == nil {
		return
	}
	global.PRISM_LOG.Warn(msg, fields...)
}

// metadataKeyAttachments message.metadata 里存附件清单的 key。
//
// 这个 key 名不是新造的：model/message.go 的文档注释早已预留 "attachments"。
const metadataKeyAttachments = "attachments"

// maxAttachmentsPerMessage 单条消息的附件数上限。
//
// 有上限是必要的：每个附件在 executor 侧都要拉取 + base64 + 一次 hermes RPC，
// 不设限会让单轮执行时间不可控。
const maxAttachmentsPerMessage = 10

// storage 附件用的 storage 客户端。nil 表示未配置 —— 此时带附件的请求会被拒绝，
// 但不带附件的对话必须照常工作（降级而非全盘失败）。
var storage *storageclient.Client

// SetStorage 注入 storage 客户端（main 在启动时调用）。
func SetStorage(c *storageclient.Client) { storage = c }

// AttachmentInput 前端上传完成后回传的附件引用。
//
// 只有 FileID 是可信输入；Name 仅作兜底，真实值一律以 storage 的元数据为准
// （前端可能篡改 size/mimeType 来绕过限制）。
type AttachmentInput struct {
	FileID string `json:"fileId" required:"true" minLength:"1" doc:"storage presign 返回的文件ID"`
	Name   string `json:"name,omitempty" doc:"原始文件名（展示用，最终以 storage 记录为准）"`
}

// resolveAttachments 把前端回传的引用核对成可信的附件清单。
//
// 逐个向 storage 查元数据：确认存在、已 active、属于本命名空间，并用 storage 的
// 权威值覆盖名称/大小/类型。任一附件不合法即整体失败 —— 部分成功会让用户以为
// 文件都传上去了，而 agent 只看到一部分，比直接报错更难排查。
func resolveAttachments(ctx context.Context, in []AttachmentInput) ([]a2a.Attachment, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxAttachmentsPerMessage {
		return nil, fmt.Errorf("%w: 数量超过上限（%d）", ErrInvalidAttachment, maxAttachmentsPerMessage)
	}
	if storage == nil {
		return nil, fmt.Errorf("%w: 存储服务未配置", ErrInvalidAttachment)
	}

	out := make([]a2a.Attachment, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		if item.FileID == "" {
			return nil, fmt.Errorf("%w: fileId 不能为空", ErrInvalidAttachment)
		}
		// 去重：同一文件重复引用会让 hermes 侧重复 attach，浪费 token 且可能
		// 让 agent 误以为有多个文件。
		if _, dup := seen[item.FileID]; dup {
			continue
		}
		seen[item.FileID] = struct{}{}

		f, err := storage.Stat(ctx, item.FileID)
		if err != nil {
			return nil, fmt.Errorf("%w (%s): %v", ErrInvalidAttachment, item.FileID, err)
		}
		name := f.OrigName
		if name == "" {
			name = item.Name
		}
		out = append(out, a2a.Attachment{
			FileID:   f.FileID,
			Name:     name,
			MimeType: f.MimeType,
			Size:     f.Size,
			SHA256:   f.SHA256,
			// Kind 只在此处归一一次，executor 直接用结果（见 a2a.Attachment.Kind）。
			Kind: a2a.AttachmentKindForMime(f.MimeType),
		})
	}
	return out, nil
}

// attachmentsToMetadata 把附件清单编成 message.metadata 的 JSON。
//
// 刻意剥掉 URL：签名链接有效期有限，存进 DB 只会留下死链。
func attachmentsToMetadata(atts []a2a.Attachment) model.JSON {
	if len(atts) == 0 {
		return nil
	}
	stripped := make([]a2a.Attachment, 0, len(atts))
	for _, a := range atts {
		a.URL = ""
		stripped = append(stripped, a)
	}
	return model.MustNewJSON(map[string]any{metadataKeyAttachments: stripped})
}

// attachmentsFromMetadata 从 message.metadata 解出附件清单。
//
// 解析失败按「无附件」处理并记日志：一条元数据坏掉不该让整个对话的历史组装失败。
func attachmentsFromMetadata(meta model.JSON) []a2a.Attachment {
	if len(meta) == 0 {
		return nil
	}
	var wrapper struct {
		Attachments []a2a.Attachment `json:"attachments"`
	}
	if err := json.Unmarshal(meta, &wrapper); err != nil {
		warnAttach("conversation: 解析 message.metadata 附件失败", zap.Error(err))
		return nil
	}
	return wrapper.Attachments
}

// signAttachments 为每个附件签一条下载直链（就地写回元素的 URL）。
//
// 单个附件签发失败不中断：executor 侧会跳过没有 URL 的附件并提示，
// 让「一个附件坏掉」退化为「少一个附件」而不是「整轮任务失败」。
func signAttachments(ctx context.Context, atts []a2a.Attachment) {
	if len(atts) == 0 || storage == nil {
		return
	}
	for i := range atts {
		res, err := storage.PresignDownload(ctx, atts[i].FileID)
		if err != nil {
			warnAttach("conversation: 签发附件下载地址失败",
				zap.String("fileId", atts[i].FileID), zap.Error(err))
			continue
		}
		atts[i].URL = res.URL
	}
}

package llmproxy

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestLimitWriterUnderLimit 验证未超限时捕获全部内容。
func TestLimitWriterUnderLimit(t *testing.T) {
	lw := newLimitWriter(100)
	lw.Write([]byte("hello"))
	if got := string(lw.Bytes()); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
	if lw.drop {
		t.Error("should not drop under limit")
	}
}

// TestLimitWriterOverLimit 验证超过限制后丢弃且报告写入成功（让 TeeReader 继续）。
func TestLimitWriterOverLimit(t *testing.T) {
	lw := newLimitWriter(5)
	// 写 10 字节，前 5 捕获，后 5 丢弃。
	n, err := lw.Write([]byte("0123456789"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 10 {
		t.Errorf("reported written = %d, want 10 (must not block tee)", n)
	}
	if got := string(lw.Bytes()); got != "01234" {
		t.Errorf("captured = %q, want 01234", got)
	}
	if !lw.drop {
		t.Error("should be dropping after limit exceeded")
	}
	// 继续写也安全（丢弃）。
	n, err = lw.Write([]byte("more"))
	if err != nil || n != 4 {
		t.Errorf("post-limit write n=%d err=%v", n, err)
	}
}

// TestLimitWriterMultipleWrites 验证多次小写入累积到限制。
func TestLimitWriterMultipleWrites(t *testing.T) {
	lw := newLimitWriter(8)
	for _, b := range []string{"ab", "cd", "ef", "gh", "ij"} {
		lw.Write([]byte(b))
	}
	if got := string(lw.Bytes()); got != "abcdefgh" {
		t.Errorf("captured = %q, want abcdefgh", got)
	}
	if !lw.drop {
		t.Error("should drop after 8 bytes + overflow")
	}
}

// TestStreamingProxyTeeStreamsBody 验证 TeeReader 模式：客户端收到完整流，缓冲只存前缀。
//
// 这是 CRITICAL 修复的回归测试：proxy 不能用 io.ReadAll 缓冲整个 body（会破坏 SSE）。
func TestStreamingProxyTeeStreamsBody(t *testing.T) {
	const maxCapture = 32
	// 模拟上游 SSE 流（远超 maxCapture）。
	fullStream := strings.Repeat("data: chunk\n\n", 100) // ~1200 bytes
	upstream := io.NopCloser(strings.NewReader(fullStream))

	// 用 TeeReader + limitWriter 复刻 proxy.ModifyResponse 的逻辑。
	lw := newLimitWriter(maxCapture)
	teeReader := io.TeeReader(upstream, lw)

	// 客户端读取（流式）。
	var client bytes.Buffer
	io.Copy(&client, teeReader)

	// 客户端应收到完整流（未被缓冲截断）。
	if client.Len() != len(fullStream) {
		t.Errorf("client received %d bytes, want %d (streaming broken)", client.Len(), len(fullStream))
	}
	if client.String() != fullStream {
		t.Error("client stream content mismatch")
	}
	// 缓冲只存前缀（用于日志）。
	if len(lw.Bytes()) != maxCapture {
		t.Errorf("captured prefix = %d bytes, want %d", len(lw.Bytes()), maxCapture)
	}
}

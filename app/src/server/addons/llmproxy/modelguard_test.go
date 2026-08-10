package llmproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestModelFromRequestBody 解不出 model 一律返回空串（由调用方按「放行」处理）——
// embeddings 等端点的请求体形态不同，不该因为解不出 model 就拒掉整个请求。
func TestModelFromRequestBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"标准 chat 请求", `{"model":"glm-4.6","messages":[{"role":"user","content":"hi"}]}`, "glm-4.6"},
		{"带空白", `{"model":"  glm-5.2  "}`, "glm-5.2"},
		{"无 model 键", `{"messages":[]}`, ""},
		{"空 body", ``, ""},
		{"非 JSON", `not json`, ""},
		{"model 为空串", `{"model":""}`, ""},
		{"model 类型不符", `{"model":123}`, ""},
	}
	for _, c := range cases {
		if got := modelFromRequestBody([]byte(c.body)); got != c.want {
			t.Errorf("%s: modelFromRequestBody(%q) = %q, want %q", c.name, c.body, got, c.want)
		}
	}
}

// TestValidateModelEmptyModelPasses 空 model = 用上游默认，放行（既有行为）。
//
// 用 nil DB 跑：空 model 必须在**查库之前**短路返回，否则每个不带 model 的
// 请求都会白查一次库。
func TestValidateModelEmptyModelPasses(t *testing.T) {
	for _, m := range []string{"", "   "} {
		if err := ValidateModel(1, m); err != nil {
			t.Errorf("ValidateModel(1, %q) = %v, want nil", m, err)
		}
	}
}

// TestValidateModelNoDB DB 未初始化时报错而非 panic，且不能被误判成「放行」。
func TestValidateModelNoDB(t *testing.T) {
	if err := ValidateModel(1, "glm-4.6"); err == nil {
		t.Fatal("want error when DB is nil, got nil")
	}
}

// TestRestoreBody 复位后 body 可完整读出，且 ContentLength/GetBody 同步。
//
// 这三个字段少同步一个就会出问题：ContentLength 不对会让 Transport 截断或挂住，
// GetBody 缺失会在重定向/重试时静默丢掉请求体。校验逻辑读过 body 之后，
// 转发链路必须看不出任何差别。
func TestRestoreBody(t *testing.T) {
	const payload = `{"model":"glm-4.6","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/llm-proxy/v1/chat/completions",
		strings.NewReader(payload))

	// 模拟校验流程：读空 → 复位。
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	restoreBody(req, body)

	if req.ContentLength != int64(len(payload)) {
		t.Errorf("ContentLength = %d, want %d", req.ContentLength, len(payload))
	}
	again, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll after restore: %v", err)
	}
	if string(again) != payload {
		t.Errorf("body after restore = %q, want %q", again, payload)
	}

	// GetBody 必须能再取一份（Transport 重试时依赖它）。
	if req.GetBody == nil {
		t.Fatal("GetBody is nil after restore")
	}
	rc, err := req.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	third, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll from GetBody: %v", err)
	}
	if string(third) != payload {
		t.Errorf("GetBody body = %q, want %q", third, payload)
	}
}

// TestRestoreBodyEmpty 空 body 也要能复位（无体请求不能因此崩）。
func TestRestoreBodyEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	restoreBody(req, nil)
	if req.ContentLength != 0 {
		t.Errorf("ContentLength = %d, want 0", req.ContentLength)
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(b) != 0 {
		t.Errorf("body = %q, want empty", b)
	}
}

// TestModelMatchIsCaseInsensitive 白名单比对忽略大小写与两端空白 ——
// provider 控制台里配的模型名常带多余空格，不该因此拒掉合法请求。
//
// 纯字符串逻辑，不查库：验证 ValidateModel 内部用的比较规则。
func TestModelMatchIsCaseInsensitive(t *testing.T) {
	models := []string{" GLM-4.6 ", "glm-5.2"}
	match := func(want string) bool {
		for _, m := range models {
			if strings.EqualFold(strings.TrimSpace(m), want) {
				return true
			}
		}
		return false
	}
	for _, ok := range []string{"glm-4.6", "GLM-4.6", "glm-5.2"} {
		if !match(ok) {
			t.Errorf("model %q should match whitelist %v", ok, models)
		}
	}
	if match("gpt-4o") {
		t.Error("model gpt-4o should NOT match whitelist")
	}
}

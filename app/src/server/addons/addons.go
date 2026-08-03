package addons

// 导入此包会触发所有业务插件 init()。
import (
	// 核心数据迁移 + seed（Provider/Agent/Skill/Tool/CallLog/Project）
	_ "nucleagent-core/addons/coredata"
	// LLM 代理：TempLLMKey + 反向代理 + CallLog
	_ "nucleagent-core/addons/llmproxy"
	// Executor 注册 + WebSocket 服务端
	_ "nucleagent-core/addons/executorreg"
	// 对话编排：CRUD + SSE + A2A 调度 + executorreg.Handler 实现
	_ "nucleagent-core/addons/conversation"
	// Agent 模板只读：GET /api/v1/addons/agent/templates（前端创作/任务视图用）
	_ "nucleagent-core/addons/agent"
	// Skill 只读：GET /api/v1/addons/skill
	_ "nucleagent-core/addons/skill"
)

# nucleagent-core

核心引擎：任务编排与生命周期管理。基于 Prism Fusion 框架。

## 构建

```bash
cd app/src/server
go work sync          # 同步 prism-fusion submodule
go build ./...
go vet ./...
go run main.go        # 启动（需要 MySQL + Redis）
```

## 架构约束

- 所有业务逻辑必须作为 Prism Fusion addon 实现（`plugin.Plugin` 接口 + `init()` 注册）
- addon 间禁止直接 import，通过 `global` 变量或 service 接口通信
- GORM model 定义在 `nucleagent-shared`，不在本 repo 定义
- 路由注册在 addon 的 `RegisterRoutes()` 里，用 Huma OpenAPI
- 配置用 Viper，写在 `config.yaml`

## Addons

| addon | 职责 |
|-------|------|
| conversation | Conversation/Message/Step CRUD + SSE 流 |
| a2a | A2A 编排 (a2a / a2a_agent / a2a_employee) |
| agent | AgentTemplate/AgentInstance 管理 |
| skill | Skill + SkillBinding 管理 |
| tool | Tool 管理 |
| llm-proxy | LLM 代理 (TempLLMKey + Proxy 端点) |
| mcp | MCP 注册表 |
| executor-reg | Executor 注册 + 心跳 + WebSocket |
| project | 项目管理 |

## 依赖

- `nucleagent-shared` (GORM model + 协议) via go.work replace
- `prism-fusion` (框架) via git submodule + go.work
- MySQL (业务表), Redis (SSE pub/sub + 缓存)
- `nucleagent-auth` (共享 JWT secret，本地验证，不远程调用)

## 边界

- **Always**: 新 addon 必须在 `addons/addons.go` 里 import
- **Always**: SSE 消息通过 Redis pub/sub（多实例支持）
- **Ask first**: 新增数据表（先改 nucleagent-shared + docs）
- **Never**: 禁止在 Engine 端做 LLM 推理循环（全部推给 Executor）
- **Never**: 禁止直接连 Executor 的数据库（Executor 不连数据库）

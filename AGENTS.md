# nucleagent-core

核心引擎：任务编排与生命周期管理。基于 Prism Fusion 框架。


## Commit Message Language (IRON RULE)

**All commit messages MUST be written in English.** No exceptions.

- Subjects and bodies: English only. No Chinese characters anywhere in the message.
- Type prefixes follow Conventional Commits (`feat:`, `fix:`, `chore:`, `refactor:`, `docs:`, `style:`, `perf:`, `test:`).
- Referencing code identifiers, paths, or domain terms is fine; prose must be English.

Rationale: these repositories are open-sourced on GitHub; Chinese commit messages
make history unreadable to international contributors and pollute git log tooling.

## 构建

> 前置：首次构建需先在 repo 根目录执行 `git submodule update --init` 拉取 prism-fusion，再 `go work sync`。

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
| executor-reg | Executor 注册 + 心跳 + WebSocket 连接管理 |
| project | 项目管理 |

> tool 与 mcp 边界：tool 管 Tool 记录 CRUD（元数据/配置持久化）；mcp 管 MCP server 连接注册（运行时连接生命周期）。

## 依赖

- `nucleagent-shared` (GORM model + 协议) via go.work replace
- `prism-fusion` (框架) via git submodule + go.work
- MySQL (业务表), Redis (SSE pub/sub + LLM Proxy 临时 Key 存储)
- `nucleagent-auth` (共享 JWT secret，本地验证，不远程调用)

## API 约定

- **路由前缀**：所有业务路由统一挂载在 `/api/v1/addons/` 下（如 `/api/v1/addons/conversation`）；S2S 路由在 `/api/v1/addons/s2s/`
- **S2S 认证**：core ↔ executor 通信用 `X-Executor-Token` 请求头校验（Executor 注册时签发，心跳/WebSocket 携带）
- **错误格式**：统一返回 `{ "code": "<ERROR_CODE>", "message": "<人类可读说明>" }`（如 `CONVERSATION_NOT_FOUND`）
- **CORS**：`cors.mode: strict-whitelist`，通过环境变量配置允许的前端来源（WEB_FRONTEND_URL + CORE_FRONTEND_URL），支持分布式部署

## 边界

- **Always**: 新 addon 必须在 `addons/addons.go` 里 import
- **Always**: SSE 消息通过 Redis pub/sub（多实例支持）
- **Ask first**: 新增数据表（先改 nucleagent-shared + docs）
- **Never**: 禁止在 core 端做 LLM 推理循环（全部推给 Executor）
- **Never**: 禁止直接连 Executor 的数据库（Executor 不连数据库）

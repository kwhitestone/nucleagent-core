package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"nucleagent-core/addons/llmproxy"

	"whitestone.top/prism-fusion/core"
	"whitestone.top/prism-fusion/global"
	"whitestone.top/prism-fusion/initialize"

	// 框架内置 auth/rbac（core 需要 JWT 认证 + RBAC）。
	_ "whitestone.top/prism-fusion/addons"
	// nucleagent-core 业务插件：coredata / llmproxy / executorreg / conversation。
	_ "nucleagent-core/addons"
)

// expandNucleagentEnv 展开 config.yaml 的 nucleagent 段里未展开的 ${VAR} 字面量。
//
// prism-fusion 的 expandEnvInConfig 只展开 mysql 段；nucleagent 段（executor-token /
// executor-url / redis-addr / public-url）在 config.yaml 里用 ${VAR} 写法，需在此手动展开。
func expandNucleagentEnv() {
	if global.PRISM_VP == nil {
		return
	}
	for _, key := range []string{
		"nucleagent.executor-token",
		"nucleagent.executor-url",
		"nucleagent.redis-addr",
		"nucleagent.public-url",
	} {
		raw := global.PRISM_VP.GetString(key)
		expanded := expandEnv(raw)
		if expanded != raw {
			global.PRISM_VP.Set(key, expanded)
		}
	}
}

// expandEnv 支持 ${VAR} 和 ${VAR:-default} 的环境变量展开。
func expandEnv(s string) string {
	return os.Expand(s, func(k string) string {
		if i := strings.Index(k, ":-"); i >= 0 {
			envKey, def := k[:i], k[i+2:]
			if v := os.Getenv(envKey); v != "" {
				return v
			}
			return def
		}
		return os.Getenv(k)
	})
}

func main() {
	initializeSystem()

	// 启动 TempLLMKey 定期清理（防内存累积）。
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	llmproxy.Default.StartCleanupLoop(rootCtx)

	// 运行 HTTP 服务器（阻塞）。
	core.RunServer()
}

func initializeSystem() {
	global.PRISM_VP = core.Viper()
	global.PRISM_LOG = core.Zap()
	zap.ReplaceGlobals(global.PRISM_LOG)
	expandNucleagentEnv() // 展开 nucleagent 段的 ${VAR}（框架只展开 mysql 段）
	global.PRISM_DB = initialize.Gorm()
	if global.PRISM_DB != nil {
		global.PRISM_LOG.Info("Database connected successfully")
		initialize.InitTables()
	}
	global.PRISM_LOG.Info("nucleagent-core initialized")
}

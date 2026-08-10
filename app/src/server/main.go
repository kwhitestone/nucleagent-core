package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"nucleagent-core/addons/conversation/svc"
	"nucleagent-core/addons/llmproxy"
	"nucleagent-core/internal/storageclient"

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
		"nucleagent.storage-url",
		"nucleagent.master-key",
	} {
		raw := global.PRISM_VP.GetString(key)
		expanded := expandEnv(raw)
		if expanded != raw {
			global.PRISM_VP.Set(key, expanded)
		}
	}
}

// storageNamespace core 在 storage 侧的命名空间。
//
// 与 storage 的 config.yaml namespaces 配置对应（core / executor 各一个前缀），
// 决定文件落在 CS 的哪个路径下，也是跨服务访问隔离的依据。
const storageNamespace = "core"

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

	// MASTER_KEY 自检：缺失/格式错误直接退出，绝不带病启动。
	//
	// 这个值不可能在运行时被补上——带着它跑起来，只会把失败推迟到用户发消息
	// 那一刻，且表现为 LLM 调用失败，极难定位到「环境变量没配」这个真因。
	// 参照 storage 对 CS 凭据的 fail-fast 处理（见 deploy/scripts/dev.sh）。
	if err := llmproxy.ValidateMasterKey(); err != nil {
		global.PRISM_LOG.Fatal("MASTER_KEY 未配置或格式错误——core 无法解密 providers.api_key，拒绝启动。"+
			"请在 nucleagent-deploy/.env 设置 MASTER_KEY（生成：openssl rand -hex 32）。"+
			"注意：更换该值会使已有 provider 密文永久不可解，需重新录入各 provider 的 API key。",
			zap.Error(err))
	}

	// 启动 TempLLMKey 存储：Redis 可达走 Redis（重启/多实例不丢 key），否则内存兜底。
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	redisAddr := global.PRISM_VP.GetString("nucleagent.redis-addr")
	if llmproxy.InitDefault(redisAddr) {
		global.PRISM_LOG.Info("TempLLMKey store: redis", zap.String("addr", redisAddr))
	} else {
		global.PRISM_LOG.Info("TempLLMKey store: memory (redis unavailable or unconfigured)")
	}
	llmproxy.Default.StartCleanupLoop(rootCtx)

	// 注入 storage 客户端（对话附件用）。地址为空时 New 返回 nil，
	// 附件功能不可用但普通对话照常 —— 存储服务缺失不该拖垮整个 core。
	storageURL := global.PRISM_VP.GetString("nucleagent.storage-url")
	if sc := storageclient.New(storageURL, storageNamespace); sc != nil {
		svc.SetStorage(sc)
		global.PRISM_LOG.Info("storage client ready", zap.String("url", storageURL))
	} else {
		global.PRISM_LOG.Warn("storage 未配置，对话附件功能不可用")
	}

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

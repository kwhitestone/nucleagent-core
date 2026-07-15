package main

import (
	"nucleagent_core/core"
	"nucleagent_core/global"
	"nucleagent_core/initialize"

	"go.uber.org/zap"

	_ "whitestone.top/prism-fusion/addons"
	_ "nucleagent_core/addons"
)

func main() {
	initializeSystem()
	core.RunServer()
}

func initializeSystem() {
	global.PRISM_VP = core.Viper()
	global.PRISM_LOG = core.Zap()
	zap.ReplaceGlobals(global.PRISM_LOG)
	global.PRISM_DB = initialize.Gorm()
	if global.PRISM_DB != nil {
		global.PRISM_LOG.Info("Database connected successfully")
		initialize.InitTables()
	}
	global.PRISM_LOG.Info("nucleagent-core initialized")
}

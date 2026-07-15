module nucleagent-core

go 1.25

require (
	github.com/nucleagent/nucleagent-shared v0.0.0
	whitestone.top/prism-fusion v0.0.0
	gorm.io/driver/mysql v1.6.0
	gorm.io/gorm v1.31.1
	github.com/gin-gonic/gin v1.11.0
	github.com/gorilla/websocket v1.5.3
	github.com/redis/go-redis/v9 v9.6.1
)

replace (
	github.com/nucleagent/nucleagent-shared => ../../nucleagent-shared
	whitestone.top/prism-fusion => ./prism-fusion/src/server
)

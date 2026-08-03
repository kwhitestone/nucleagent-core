# nucleagent-core

Nucleagent 核心引擎。任务聚合与生命周期管理。

基于 [Prism Fusion](https://github.com/kwhitestone/prism-fusion) 框架构建。

## 结构

```
nucleagent-core/
├── prism-fusion/              git submodule
├── app/
│   ├── src/
│   │   ├── server/            Go 后端
│   │   │   ├── addons/        业务插件
│   │   │   ├── go.work
│   │   │   ├── go.mod
│   │   │   ├── config.yaml
│   │   │   └── main.go
│   │   └── web/               Vue 前端 (micro-app 子应用)
│   └── Dockerfile
└── README.md
```

## 端口

- 后端: 26680
- 前端: 26688

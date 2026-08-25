基于 Go 实现的杂交水稻种子放播检验 Web 项目，一款前后端应用，完成批次锁定、抽样检测、异常复判与放播裁定。

# riceguard-hybrid-seed-release-gate

## 本地构建与测试

```bash
go mod download
go build ./...
go test ./...
./run_benzhi_smoke.sh
```

## Docker 构建与运行

```bash
./build_benzhi_docker.sh riceguard-hybrid-seed-release-gate linux/arm64
docker run --rm -it --platform linux/arm64 riceguard-hybrid-seed-release-gate:latest
./build_benzhi_docker.sh riceguard-hybrid-seed-release-gate linux/amd64
docker run --rm -it --platform linux/amd64 riceguard-hybrid-seed-release-gate:latest
```

构建脚本第二个参数为目标平台，必须分别完成 linux/arm64 和 linux/amd64 构建与容器验证；未提供时按照规范默认使用 linux/amd64。系统 frontend-v2 模板通过 Go 原生交叉编译生成目标架构的 /usr/local/bin/benzhi-app，镜像默认直接运行该入口。

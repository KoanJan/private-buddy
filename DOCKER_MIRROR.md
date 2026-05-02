# Docker 镜像拉取问题解决方案

## 问题描述

遇到 `403 Forbidden` 错误，无法从 Docker 镜像源拉取镜像。

## 解决方案

### 方案1：配置 Docker 镜像加速器（推荐）

#### macOS (Docker Desktop)

1. 打开 Docker Desktop
2. 点击右上角齿轮图标 -> Settings
3. 选择 Docker Engine
4. 在 JSON 配置中添加：

```json
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://dockerhub.azk8s.cn",
    "https://reg-mirror.qiniu.com"
  ]
}
```

5. 点击 "Apply & Restart"

#### Linux

1. 编辑 Docker 配置文件：

```bash
sudo vim /etc/docker/daemon.json
```

2. 添加镜像配置：

```json
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://dockerhub.azk8s.cn",
    "https://reg-mirror.qiniu.com"
  ]
}
```

3. 重启 Docker：

```bash
sudo systemctl daemon-reload
sudo systemctl restart docker
```

### 方案2：使用国内基础镜像

如果镜像加速器仍然无法解决问题，可以修改 Dockerfile 使用国内镜像：

#### server/Dockerfile

```dockerfile
FROM swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/python:3.11-slim
```

#### web/Dockerfile

```dockerfile
FROM swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/node:18-alpine AS build
FROM swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/nginx:alpine
```

### 方案3：手动拉取镜像

先手动拉取镜像，再构建：

```bash
# 拉取基础镜像
docker pull python:3.11-slim
docker pull node:18-alpine
docker pull nginx:alpine

# 然后构建
docker-compose build
```

### 方案4：使用代理

如果有 HTTP 代理，可以配置 Docker 使用代理：

#### macOS (Docker Desktop)

1. Settings -> Resources -> Proxies
2. Enable Manual proxy configuration
3. 配置代理地址

#### Linux

```bash
sudo mkdir -p /etc/systemd/system/docker.service.d
sudo vim /etc/systemd/system/docker.service.d/http-proxy.conf
```

添加：

```ini
[Service]
Environment="HTTP_PROXY=http://proxy.example.com:8080"
Environment="HTTPS_PROXY=http://proxy.example.com:8080"
Environment="NO_PROXY=localhost,127.0.0.1"
```

然后重启：

```bash
sudo systemctl daemon-reload
sudo systemctl restart docker
```

## 验证配置

测试镜像拉取是否正常：

```bash
docker pull hello-world
```

## 常用国内镜像源

- 中科大：https://docker.mirrors.ustc.edu.cn
- 七牛云：https://reg-mirror.qiniu.com
- 阿里云：https://xxx.mirror.aliyuncs.com (需要登录获取专属地址)
- 腾讯云：https://mirror.ccs.tencentyun.com

## 注意事项

1. 某些镜像源可能不稳定，建议配置多个镜像源
2. 如果使用公司网络，可能需要配置代理
3. 镜像源配置后需要重启 Docker 才能生效

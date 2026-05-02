# Docker 网络配置说明

## 网络访问需求

Private Buddy 后端需要访问以下外部服务：

1. **LLM API**（如 OpenAI API、其他兼容 API）
   - 需要访问公网 HTTPS 端点
   - 默认端口：443

2. **搜索服务**（DuckDuckGo）
   - 需要访问公网 HTTPS 端点
   - 用于网络搜索功能

3. **Hugging Face**（模型下载）
   - 首次启动时下载 BGE-base-zh 模型
   - 需要访问 huggingface.co

## Docker 网络模式

### 默认模式（Bridge）

Docker 容器默认使用 bridge 网络模式，可以正常访问外网。

**优点**：
- 隔离性好
- 端口映射清晰

**缺点**：
- 某些网络环境下可能有 DNS 解析问题
- 可能需要配置代理

### Host 模式（可选）

如果遇到网络问题，可以使用 host 网络模式：

```yaml
services:
  server:
    network_mode: host
```

**优点**：
- 网络性能最好
- 无需端口映射
- DNS 解析与宿主机一致

**缺点**：
- 隔离性差
- 端口冲突风险

## 网络问题排查

### 1. DNS 解析问题

**症状**：容器无法解析域名

**解决方案**：

#### 方案A：配置 DNS 服务器

编辑 `docker-compose.yml`：

```yaml
services:
  server:
    dns:
      - 8.8.8.8
      - 8.8.4.4
```

#### 方案B：使用宿主机 DNS

```yaml
services:
  server:
    dns:
      - 宿主机的DNS服务器
```

### 2. 代理配置

**症状**：公司网络需要代理才能访问外网

**解决方案**：

编辑 `.env` 文件：

```bash
HTTP_PROXY=http://proxy.company.com:8080
HTTPS_PROXY=http://proxy.company.com:8080
NO_PROXY=localhost,127.0.0.1,.company.com
```

### 3. 防火墙问题

**症状**：连接超时或拒绝

**解决方案**：

确保防火墙允许：
- 出站 HTTPS (443)
- DNS 查询 (53)

## 验证网络连通性

### 进入容器测试

```bash
# 进入容器
docker exec -it private-buddy-server bash

# 测试 DNS 解析
nslookup api.openai.com

# 测试 HTTPS 连接
curl -I https://api.openai.com

# 测试 Hugging Face
curl -I https://huggingface.co
```

### 从宿主机测试

```bash
# 测试容器网络
docker run --rm alpine ping -c 3 google.com
docker run --rm alpine nslookup api.openai.com
```

## 网络优化建议

### 1. 使用国内镜像源

如果访问 Hugging Face 较慢，可以配置镜像：

```bash
export HF_ENDPOINT=https://hf-mirror.com
```

在 Docker 中：

```yaml
services:
  server:
    environment:
      - HF_ENDPOINT=https://hf-mirror.com
```

### 2. 连接池配置

后端已配置 `httpx` 连接池：

```python
self._http_client = httpx.AsyncClient(
    limits=httpx.Limits(
        max_connections=1,
        max_keepalive_connections=0,
    ),
)
```

### 3. 超时设置

LLM API 调用默认有超时机制，避免长时间等待。

## 常见问题

### Q1: 容器内无法访问外网？

**检查步骤**：
1. 宿主机能否访问外网
2. Docker 服务是否正常运行
3. 防火墙是否阻止容器网络
4. DNS 配置是否正确

### Q2: LLM API 调用超时？

**可能原因**：
1. 网络延迟高
2. API 服务限流
3. 代理配置错误

**解决方案**：
1. 检查网络延迟
2. 配置合理的超时时间
3. 使用代理或 VPN

### Q3: 模型下载失败？

**可能原因**：
1. Hugging Face 访问受限
2. 磁盘空间不足
3. 网络不稳定

**解决方案**：
1. 使用 Hugging Face 镜像
2. 清理磁盘空间
3. 使用预构建镜像（已包含模型）

## 最佳实践

1. **生产环境**：
   - 配置稳定的 DNS 服务器
   - 设置合理的超时时间
   - 使用负载均衡

2. **开发环境**：
   - 使用 bridge 模式
   - 配置代理（如需要）
   - 启用详细日志

3. **企业环境**：
   - 配置企业代理
   - 使用内部 DNS
   - 配置防火墙规则

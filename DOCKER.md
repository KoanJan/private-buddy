# Private Buddy Docker 部署文档

## 快速开始

### 1. 环境准备

确保已安装：
- Docker: https://docs.docker.com/get-docker/
- Docker Compose: https://docs.docker.com/compose/install/

### 2. 配置环境变量（可选）

```bash
# 复制环境变量模板
cp .env.example .env

# 编辑配置（可选，使用默认配置即可）
vim .env
```

### 3. 一键部署

```bash
./deploy.sh
```

或者手动部署：

```bash
docker-compose up -d --build
```

### 4. 访问应用

- Web UI: http://localhost
- API: http://localhost:8000

## 配置说明

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| DATA_ROOT | 数据存储根目录 | ~/PBD_trial_docker_and_embedding |
| LOG_LEVEL | 日志级别 | INFO |
| SUMMARY_WINDOW_SIZE | 摘要窗口大小 | 5 |
| TASK_MAX_ITERATIONS | 任务最大迭代次数 | 50 |
| CONTEXT_WINDOW_ITERATIONS | 上下文窗口迭代次数 | 10 |
| NOTES_MAX_CHARS | 笔记最大字符数 | 5000 |

### 数据持久化

所有数据存储在Docker volume `buddy-data` 中，包括：
- SQLite数据库
- ChromaDB向量存储
- 工作空间文件
- Agent头像

## 常用命令

```bash
# 查看日志
docker-compose logs -f

# 查看特定服务日志
docker-compose logs -f server
docker-compose logs -f web

# 停止服务
docker-compose down

# 重启服务
docker-compose restart

# 重新构建并启动
docker-compose up -d --build

# 进入容器
docker-compose exec server bash
docker-compose exec web sh

# 清理所有数据（危险操作！）
docker-compose down -v
```

## 更新部署

```bash
# 拉取最新代码
git pull

# 重新构建并启动
docker-compose up -d --build
```

## 故障排查

### 端口冲突

如果80或8000端口被占用，可以修改 `docker-compose.yml` 中的端口映射：

```yaml
services:
  web:
    ports:
      - "8080:80"  # 将80改为8080
  server:
    ports:
      - "8001:8000"  # 将8000改为8001
```

### 数据丢失

确保使用Docker volume进行数据持久化。检查volume是否存在：

```bash
docker volume ls | grep buddy-data
```

### 日志查看

查看详细日志以排查问题：

```bash
docker-compose logs -f --tail=100
```

## 注意事项

1. **Embedding模型**: 
   - 使用内置的BGE-base-zh模型（768维）
   - 首次运行时会自动从Hugging Face下载模型文件（约400MB）
   - 模型会缓存到 `~/.cache/huggingface/` 目录
   - 如果网络较慢，可以设置Hugging Face镜像：
     ```bash
     export HF_ENDPOINT=https://hf-mirror.com
     ```
2. **数据目录**: 容器内使用 `/root/PBD_trial_docker_and_embedding` 作为数据目录
3. **网络访问**: 默认绑定所有网络接口，生产环境建议配置防火墙或反向代理

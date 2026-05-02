# Windows 部署指南

## 前置要求

1. **安装 Docker Desktop for Windows**
   - 下载地址：https://docs.docker.com/desktop/install/windows-install/
   - 安装完成后启动 Docker Desktop
   - 确保 Docker 正在运行（系统托盘图标）

2. **系统要求**
   - Windows 10 64-bit: Pro, Enterprise, or Education (Build 19041 or higher)
   - 或 Windows 11
   - 启用 WSL 2 或 Hyper-V（Docker Desktop 安装时会提示）

## 部署步骤

### 方式一：使用部署脚本（推荐）

1. **打开 PowerShell 或命令提示符**
   - 按 `Win + X`，选择 "Windows PowerShell" 或 "命令提示符"

2. **进入项目目录**
   ```cmd
   cd C:\path\to\private-buddy
   ```

3. **运行部署脚本**
   ```cmd
   deploy-cn.bat
   ```

脚本会自动：
- 检查 Docker 是否安装
- 创建 .env 配置文件
- 构建并启动容器

### 方式二：手动部署

1. **创建配置文件**
   ```cmd
   copy .env.example .env
   ```

2. **构建并启动**
   ```cmd
   docker-compose up -d --build
   ```

## 访问应用

部署成功后：

- **Web UI**: http://localhost
- **API 文档**: http://localhost:8000/docs

## 数据存储

数据默认存储在容器内的 `/root/PBD_trial_docker_and_embedding` 目录。

如需映射到 Windows 本地目录，编辑 `docker-compose.yml`：

```yaml
services:
  server:
    volumes:
      - ./data:/root/PBD_trial_docker_and_embedding  # 映射到项目目录下的 data 文件夹
```

## 常用命令

### 查看容器状态
```cmd
docker-compose ps
```

### 查看日志
```cmd
# 查看所有日志
docker-compose logs -f

# 只查看后端日志
docker-compose logs -f server

# 只查看前端日志
docker-compose logs -f web
```

### 停止服务
```cmd
docker-compose down
```

### 重启服务
```cmd
docker-compose restart
```

### 重新构建
```cmd
docker-compose build
docker-compose up -d
```

## 常见问题

### 1. Docker 未启动

**错误信息**：`error during connect: This error may indicate that the docker daemon is not running.`

**解决方案**：
- 启动 Docker Desktop
- 等待 Docker 完全启动（系统托盘图标稳定）
- 重新运行部署脚本

### 2. 端口被占用

**错误信息**：`Error starting userland proxy: listen tcp4 0.0.0.0:80: bind: address already in use`

**解决方案**：

**方案 A：停止占用端口的服务**
```cmd
# 查看占用 80 端口的进程
netstat -ano | findstr :80

# 停止进程（替换 PID）
taskkill /PID <PID> /F
```

**方案 B：修改端口**
编辑 `docker-compose.yml`：
```yaml
services:
  web:
    ports:
      - "8080:80"  # 改为 8080 端口
```

然后访问 http://localhost:8080

### 3. WSL 2 未安装

**错误信息**：`WSL 2 installation is incomplete.`

**解决方案**：
1. 以管理员身份打开 PowerShell
2. 运行：
   ```powershell
   wsl --install
   ```
3. 重启电脑
4. 重新运行部署脚本

### 4. 镜像拉取失败

**错误信息**：`failed to resolve source metadata for docker.io/library/...`

**解决方案**：

**配置 Docker 镜像加速器**：

1. 打开 Docker Desktop
2. 点击右上角齿轮图标 → Settings
3. 选择 Docker Engine
4. 在 JSON 配置中添加：

```json
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://dockerhub.azk8s.cn"
  ]
}
```

5. 点击 "Apply & Restart"
6. 重新运行部署脚本

### 5. 磁盘空间不足

**错误信息**：`no space left on device`

**解决方案**：

**清理 Docker 缓存**：
```cmd
# 清理未使用的镜像、容器、网络
docker system prune -a

# 清理构建缓存
docker builder prune -a
```

## 性能优化

### 使用 Windows 本地目录

将数据映射到 Windows 本地目录，提高性能：

```yaml
services:
  server:
    volumes:
      - ./data:/root/PBD_trial_docker_and_embedding
```

### 调整 Docker 资源

1. 打开 Docker Desktop
2. Settings → Resources
3. 调整：
   - **Memory**: 建议 4GB 以上
   - **CPU**: 建议 2 核以上
   - **Disk image location**: 选择 SSD 磁盘

## 开发环境

### 本地开发（不使用 Docker）

1. **安装依赖**
   ```cmd
   # 后端
   cd server
   python -m venv venv
   venv\Scripts\activate
   pip install -e .

   # 前端
   cd web
   npm install
   ```

2. **启动服务**
   ```cmd
   # 后端
   cd server
   venv\Scripts\activate
   uvicorn app.main:app --reload

   # 前端（新终端）
   cd web
   npm run dev
   ```

3. **访问**
   - 前端：http://localhost:5173
   - 后端：http://localhost:8000

## 生产环境建议

1. **使用 HTTPS**
   - 配置反向代理（如 Nginx）
   - 使用 Let's Encrypt 证书

2. **数据备份**
   ```cmd
   # 备份数据目录
   xcopy C:\path\to\data C:\backup\data-%date% /E /I
   ```

3. **定期更新**
   ```cmd
   git pull
   docker-compose build
   docker-compose up -d
   ```

## 技术支持

如遇到问题，请查看：
1. Docker Desktop 日志
2. 容器日志：`docker-compose logs -f`
3. 项目 Issues：https://github.com/your-org/private-buddy/issues

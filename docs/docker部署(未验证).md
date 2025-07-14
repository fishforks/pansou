## 快速开始

### 使用Docker部署

#### 方法1：使用Docker Compose（推荐）

1. 下载docker-compose.yml文件

```bash
wget https://raw.githubusercontent.com/fish2018/pansou/main/docker-compose.yml
```

2. 启动服务

```bash
docker-compose up -d
```

3. 访问服务

```
http://localhost:8080
```

#### 方法2：直接使用Docker命令

```bash
docker run -d --name pansou \
  -p 8080:8080 \
  -v pansou-cache:/app/cache \
  -e CHANNELS="tgsearchers2,SharePanBaidu,yunpanxunlei" \
  -e CACHE_ENABLED=true \
  -e ASYNC_PLUGIN_ENABLED=true \
  ghcr.io/fish2018/pansou:latest
```
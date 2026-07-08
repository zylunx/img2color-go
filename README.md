# img2color

轻量级图片主色调提取 API，支持 Vercel Serverless、Docker 和本地运行。

## 功能

- `GET /api?img=<url>` 从图片 URL 提取主色调和调色板
- 支持 JPEG、PNG、GIF、BMP、WebP 自动解码
- 支持 `hex`、`rgb`、`hsl`、`cmyk` 输出
- 支持 Median Cut 和 K-Means：`algorithm=mediancut|kmeans`
- 支持 Referer 白名单、IP 限流、并发限制、下载大小和像素限制
- 可选 Redis 或 MongoDB 缓存
- 提供 `/health` 和 `/metrics`

AVIF 会被识别为图片容器，但当前构建不内置 AVIF 解码器，以避免 Vercel 构建引入 CGO 或重型依赖；请求会返回 `415`。

## 本地运行

```bash
go mod tidy
go run ./cmd/server
```

示例：

```bash
curl "http://localhost:8080/api?img=https%3A%2F%2Fexample.com%2Fimage.jpg&format=hex&colors=5"
```

## Vercel 部署

项目包含 `vercel.json` 和 `api/img2color.go`。配置环境变量后直接部署：

```bash
vercel deploy
```

## Docker

```bash
docker build -t img2color .
docker run --rm -p 8080:8080 --env-file .env.example img2color
```

## API

### `GET /api`

参数：

- `img`：必填，完整图片 URL
- `format`：`hex`、`rgb`、`hsl`、`cmyk`，默认 `hex`
- `colors`：返回颜色数量，范围 `1-10`，默认 `1`
- `maxWidth`：缩放最长边，默认 `400`
- `skipBackground`：是否忽略边缘 10% 区域，默认 `false`
- `algorithm`：`mediancut` 或 `kmeans`，默认 `mediancut`

响应：

```json
{
  "code": 0,
  "data": {
    "dominant": "#FF6B6B",
    "palette": [{ "color": "#FF6B6B", "ratio": 0.35 }],
    "format": "hex",
    "width": 1920,
    "height": 1080
  },
  "message": "success"
}
```

完整 OpenAPI 文档见 `docs/openapi.yaml`。

# img2color

> 轻量、快速的图片主色调提取 API — 单文件 Go 实现，一键部署到 Vercel Serverless。
>
> _A lightweight, fast dominant-color extraction API in a single Go file, ready for Vercel Serverless._

![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)
![Vercel](https://img.shields.io/badge/Deploy-Vercel-000000?logo=vercel&logoColor=white)
![Serverless](https://img.shields.io/badge/Serverless-Single%20File-4B32C3)
![License](https://img.shields.io/badge/License-MIT-green)
![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen)

通过一个 `GET` 请求，从任意图片 URL 中提取**主色调**与**调色板**，支持多种颜色格式、两种提取算法，内置限流、缓存与可观测性端点。全部逻辑收敛在 `api/img2color.go` 一个文件里，天然适配 Serverless 冷启动。

[![Deploy with Vercel](https://vercel.com/button)](https://vercel.com/new/clone?repository-url=https://github.com/zylunx/img2color-go)

---

## 目录

- [特性](#特性)
- [快速开始](#快速开始)
- [API 文档](#api-文档)
- [配置](#配置)
- [缓存](#缓存)
- [工作原理](#工作原理)
- [安全与限流](#安全与限流)
- [可观测性](#可观测性)
- [本地开发](#本地开发)
- [项目结构](#项目结构)
- [许可证](#许可证)

---

## 特性

- 🎨 **主色 + 调色板**：一次请求返回主色调与按占比排序的调色板（`ratio` 精确到小数点后 4 位）
- 🖼️ **广泛格式支持**：自动解码 JPEG、PNG、GIF、BMP、WebP、AVIF
- 🌈 **多种颜色格式**：`hex`、`rgb`、`hsl`、`cmyk` 任选
- ⚡ **两种提取算法**：Median Cut（默认，快）与 K-Means（更精细）
- 🚫 **智能过滤**：自动跳过纯黑 / 纯白结果，返回更有代表性的颜色
- 🛡️ **生产级防护**：Referer 白名单、单 IP 限流、并发上限、下载体积与像素限制、下载 / 提取超时
- 💾 **可插拔缓存**：默认内存缓存，可切换 Redis 或 MongoDB
- 📊 **开箱即用的可观测性**：`/health` 健康检查与 `/metrics` Prometheus 指标
- ☁️ **零配置部署**：单文件 + `vercel.json`，一键上线

## 快速开始

### 一键部署

点击上方 **Deploy with Vercel** 按钮，或使用 CLI：

```bash
npm i -g vercel
vercel deploy
```

部署完成后即可调用（`img` 参数需 URL 编码）：

```bash
curl "https://your-domain.vercel.app/api?img=https%3A%2F%2Fpicsum.photos%2F1200&colors=5"
```

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "dominant": "#FF6B6B",
    "palette": [
      { "color": "#FF6B6B", "ratio": 0.3521 },
      { "color": "#4ECDC4", "ratio": 0.2843 },
      { "color": "#1A535C", "ratio": 0.1902 }
    ],
    "format": "hex",
    "width": 1920,
    "height": 1080
  }
}
```

## API 文档

完整规范见 [`docs/openapi.yaml`](docs/openapi.yaml)（OpenAPI 3.0）。

### 端点

| 方法  | 路径       | 说明                       |
| ----- | ---------- | -------------------------- |
| `GET` | `/api`     | 从图片 URL 提取颜色        |
| `GET` | `/health`  | 服务健康检查与运行统计     |
| `GET` | `/metrics` | Prometheus 文本格式指标    |

### `GET /api` 请求参数

| 参数             | 类型      | 必填 | 默认        | 取值范围                | 说明                             |
| ---------------- | --------- | :--: | ----------- | ----------------------- | -------------------------------- |
| `img`            | string    |  ✅  | —           | `http` / `https` URL    | 目标图片地址（**需 URL 编码**）  |
| `format`         | string    |  —   | `hex`       | `hex` `rgb` `hsl` `cmyk`| 颜色输出格式                     |
| `colors`         | int       |  —   | `1`         | `1` – `10`              | 返回颜色数量                     |
| `maxWidth`       | int       |  —   | `400`       | `32` – `4096`           | 下采样最长边（越小越快）         |
| `skipBackground` | bool      |  —   | `false`     | `true` / `false`        | 忽略图片边缘 10%（偏向主体颜色） |
| `algorithm`      | string    |  —   | `mediancut` | `mediancut` `kmeans`    | 颜色提取算法                     |

> ⚠️ `img` 的值必须整体 **URL 编码**后再作为参数传入。服务会保留并原样转发原图 URL 上的 query（如 `?quality`），因此带 query 的图片地址若不编码，其中的 `?` `&` 会被外层解析截断，导致参数丢失。

带 query 的图片地址示例 —— 原图为 `https://img.example.com/a.avif?quality=80&w=100`：

```bash
# 将整个原图 URL 编码后作为 img 的值
curl "https://your-domain.vercel.app/api?img=https%3A%2F%2Fimg.example.com%2Fa.avif%3Fquality%3D80%26w%3D100&colors=3"
```

### 不同 `format` 的 `color` 结构

| format | 示例                                  |
| ------ | ------------------------------------- |
| `hex`  | `"#FF6B6B"`                           |
| `rgb`  | `{ "r": 255, "g": 107, "b": 107 }`    |
| `hsl`  | `{ "h": 0, "s": 100, "l": 71 }`       |
| `cmyk` | `{ "c": 0, "m": 58, "y": 58, "k": 0 }`|

### 错误响应

所有响应统一结构为 `{ code, message, data? }`。成功时 `code = 0`，出错时 `code` 等于 HTTP 状态码：

```json
{ "code": 400, "message": "missing img parameter" }
```

| HTTP 状态 | 含义                              |
| --------- | --------------------------------- |
| `400`     | 参数缺失或无法解析                |
| `403`     | Referer 不在白名单 / 触发限流     |
| `404`     | 图片不存在                        |
| `413`     | 图片体积或像素超出上限            |
| `415`     | 不支持的图片格式                  |
| `500`     | 内部错误                          |
| `503`     | 并发请求过多                      |
| `504`     | 图片下载或颜色提取超时            |

## 配置

所有配置通过环境变量注入，可参考 [`.env.example`](.env.example)。

| 变量                  | 默认         | 说明                                             |
| --------------------- | ------------ | ------------------------------------------------ |
| `ALLOWED_REFERERS`    | `*`          | Referer 白名单，逗号分隔，支持 `*.example.com`   |
| `MAX_IMAGE_SIZE_MB`   | `10`         | 允许下载的最大图片体积（MB）                     |
| `MAX_IMAGE_PIXELS`    | `16777216`   | 允许的最大像素数（默认 4096 × 4096）             |
| `DOWN_SAMPLE_SIZE`    | `400`        | 默认下采样最长边                                 |
| `REQUEST_TIMEOUT_SEC` | `10`         | 图片下载超时（秒）                               |
| `EXTRACT_TIMEOUT_SEC` | `5`          | 颜色提取超时（秒）                               |
| `RATE_LIMIT_PER_MIN`  | `60`         | 单 IP 每分钟请求上限                             |
| `MAX_CONCURRENT`      | `50`         | 最大并发处理数                                   |
| `CACHE_TTL_HOURS`     | `168`        | 缓存有效期（小时，默认 7 天）                    |
| `USE_REDIS_CACHE`     | `false`      | 是否启用 Redis 缓存                              |
| `REDIS_ADDRESS`       | —            | Redis 地址（`host:port`）                        |
| `REDIS_PASSWORD`      | —            | Redis 密码                                       |
| `REDIS_DB`            | `0`          | Redis 数据库索引                                 |
| `USE_MONGODB`         | `false`      | 是否启用 MongoDB 缓存                            |
| `MONGO_URI`           | —            | MongoDB 连接串                                   |
| `MONGO_DB`            | `img2color`  | MongoDB 数据库名                                 |

## 缓存

服务对「相同图片 + 相同参数」的结果进行缓存，缓存键由 `md5(img) + format + colors + maxWidth + skipBackground + algorithm` 组成。

| 后端        | 启用方式                                        | 适用场景                         |
| ----------- | ----------------------------------------------- | -------------------------------- |
| **内存**    | 默认                                            | 本地 / 单实例；Serverless 下不共享 |
| **Redis**   | `USE_REDIS_CACHE=true` + `REDIS_ADDRESS`        | 生产环境，跨实例共享（推荐）     |
| **MongoDB** | `USE_MONGODB=true` + `MONGO_URI`                | 已有 Mongo 基础设施              |

选择优先级为 **Redis → MongoDB → 内存**：启用且连接成功时使用对应后端，连接失败则自动回退到内存缓存。

> ⚠️ Serverless 场景下，内存缓存仅在单个函数实例生命周期内有效、且实例间不共享。若需稳定命中率，建议配置 Redis 或 MongoDB。

## 工作原理

一次请求经过如下流水线：

1. **校验参数与 URL** — 仅接受 `http` / `https`
2. **查缓存** — 命中则直接返回
3. **下载图片** — 限时、限体积，并校验 `Content-Type` 为 `image/*`
4. **解码** — JPEG / PNG / GIF / BMP / WebP / AVIF
5. **像素校验** — 超过 `MAX_IMAGE_PIXELS` 则拒绝
6. **下采样** — 最近邻缩放到 `maxWidth`，大幅降低计算量
7. **采样像素** — `skipBackground=true` 时裁掉边缘 10% 区域
8. **提取颜色** — Median Cut 或 K-Means，多取 2 个候选色
9. **过滤纯黑 / 纯白**，按占比取前 `colors` 个
10. **格式化输出并写入缓存**

**算法对比：**

- **Median Cut**（默认）：在 RGB 颜色空间中沿最大跨度通道递归二分，速度快，适合大多数场景。
- **K-Means**：固定 12 次迭代聚类，色彩划分更贴近视觉，代价是计算量更高。

## 安全与限流

- **Referer 白名单**：请求头带 `Referer` 时校验域名；支持 `*`（全放行）与 `*.domain.com` 通配。无 `Referer` 的请求默认放行，方便服务端调用。
- **单 IP 限流**：固定 1 分钟窗口，基于 `X-Forwarded-For` / `X-Real-IP` / `RemoteAddr` 识别客户端，超限返回 `403`。
- **并发限制**：信号量控制同时处理的请求数，超出返回 `503`。
- **下载防护**：体积、像素双重上限，配合下载与提取超时，避免被超大图或慢响应拖垮。

## 可观测性

**`GET /health`** — 返回运行状态与统计：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "up",
    "requests": 1024,
    "cacheHits": 768,
    "cacheHitRate": 0.75,
    "recentErrors": 3,
    "maxConcurrent": 50,
    "rateLimitPerMin": 60
  }
}
```

**`GET /metrics`** — Prometheus 文本格式：

```text
# TYPE img2color_requests_total counter
img2color_requests_total 1024
# TYPE img2color_cache_hits_total counter
img2color_cache_hits_total 768
# TYPE img2color_errors_total counter
img2color_errors_total 3
```

## 本地开发

本项目是纯 Go 服务，只需 Go 环境即可运行，无需 Node / pnpm / Vercel CLI。根目录的 `main.go` 会复用与线上完全相同的 `api.Handler`，把它包装成一个本地 HTTP 服务：

```bash
# 拉取依赖
go mod download

# 复制并按需修改环境变量（可选，缺省会用内置默认值）
cp .env.example .env

# 启动本地服务（默认 http://localhost:8080，可用 PORT 覆盖）
go run .
```

然后测试：

```bash
curl "http://localhost:8080/api?img=https%3A%2F%2Fpicsum.photos%2F800&colors=5&format=rgb"
```

> `main.go` 启动时会读取根目录 `.env`（若存在），已存在的环境变量优先。Vercel 仅构建 `api/**/*.go`，`main.go` 只用于本地运行，不影响线上部署。

## 项目结构

```text
img2color/
├── api/
│   └── img2color.go   # 全部逻辑：路由、下载解码、提取算法、缓存、限流
├── docs/
│   └── openapi.yaml   # OpenAPI 3.0 接口规范
├── main.go            # 本地运行入口（复用 api.Handler，Vercel 部署时忽略）
├── vercel.json        # Vercel 构建与路由配置
├── .env.example       # 环境变量示例
└── go.mod
```

## 许可证

本项目基于 **MIT License** 开源。

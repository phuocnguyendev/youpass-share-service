# YouPass Share Service – URL Shortener chia sẻ bài làm

Service Golang cho tính năng chia sẻ bài làm qua link rút gọn `https://youpass.vn/s/:code`.

| Yêu cầu | Cách giải quyết |
|---|---|
| **Phản hồi nhanh** | Redis cache-aside, `singleflight` chống cache stampede, negative cache, validate format trước khi chạm Redis/DB |
| **Kiểm tra quyền (tắt/xoá link)** | Cache lưu cả trạng thái link; tắt/xoá → xoá cache ngay sau commit + delayed double delete; `Cache-Control: no-store`; 410 Gone |
| **Đếm lượt view** | Buffered channel + worker pool (không chặn request) → Redis Lua (dedupe 30 phút + INCR + HyperLogLog) → flush theo lô xuống PostgreSQL |

## Chạy bằng Docker

Yêu cầu: Docker + Docker Compose v2.

```bash
make up        # build + chạy API, PostgreSQL, Redis (migration & dữ liệu mẫu tự chạy)
make demo      # demo end-to-end 16 bước bằng curl
make test      # chạy toàn bộ test (race detector) bên trong Docker
make logs      # xem log API
make down      # dừng (giữ dữ liệu)   |   make reset: dừng + xoá sạch dữ liệu
```

Không có `make`:

```bash
docker compose up -d --build
./scripts/demo.sh
```

Sau khi chạy demo, mở link được in ở cuối (ví dụ `http://localhost:8080/s/YBTiIJcT`) trên trình duyệt để xem trang bài làm.
Đổi cổng: `API_PORT=18080 make up`, rồi `API_PORT=18080 make demo`.

## API

| Method | Endpoint | Quyền | Mô tả |
|---|---|---|---|
| `POST` | `/api/v1/shares` | Chủ bài | Tạo link — `201` mới, `200` nếu đã có (idempotent) |
| `PATCH` | `/api/v1/shares/:code` | Chủ link | `{"status":"disabled"\|"active"}` |
| `DELETE` | `/api/v1/shares/:code` | Chủ link | Xoá (soft delete) — mã không bao giờ tái sử dụng |
| `GET` | `/api/v1/shares/:code/stats` | Chủ link | `{"totalViews", "uniqueToday"}` |
| `GET` | `/api/v1/shares/:code` | Public + rate limit | `200` / `404` không tồn tại / `410` đã tắt, xoá, hết hạn |
| `GET` | `/s/:code` | Public + rate limit | Trang HTML (demo; production do Next.js SSR render) |
| `DELETE` | `/api/v1/submissions/:id` | Chủ bài | Xoá bài gốc → link chia sẻ trả `410` |
| `POST` | `/api/v1/dev/token` | Chỉ development | `{"userId":1}` → access token để test |
| `GET` | `/healthz`, `/readyz`, `/metrics` | — | Liveness, readiness (DB + Redis), Prometheus |

Dữ liệu mẫu: học viên `#1 Minh Anh` sở hữu bài `101` (Writing), `102` (Speaking); học viên `#2 Hoàng Nam` sở hữu bài `201`.

```bash
TOKEN=$(curl -s -X POST localhost:8080/api/v1/dev/token -H 'Content-Type: application/json' \
  -d '{"userId":1}' | sed -E 's/.*"accessToken":"([^"]+)".*/\1/')

curl -s -X POST localhost:8080/api/v1/shares -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"resourceType":"writing_submission","resourceId":101}'
```

## Kiến trúc: Clean Architecture

```
            ┌──────────────────────────────────────────────────────────┐
            │  infrastructure / cmd  (config, kết nối DB/Redis, main)  │
            │   ┌──────────────────────────────────────────────────┐   │
            │   │  adapter  (httpapi · pgstore · redisstore · token) │   │
            │   │   ┌──────────────────────────────────────────┐   │   │
            │   │   │  usecase  (share · submission) + PORTS    │   │   │
            │   │   │   ┌──────────────────────────────────┐   │   │   │
            │   │   │   │  domain  (Link, Viewer, Code...) │   │   │   │
            │   │   │   └──────────────────────────────────┘   │   │   │
            │   │   └──────────────────────────────────────────┘   │   │
            │   └──────────────────────────────────────────────────┘   │
            └──────────────────────────────────────────────────────────┘
                        Dependency Rule: chỉ phụ thuộc vào TRONG
```

| Lớp | Trách nhiệm | Được import |
|---|---|---|
| **domain** | Entity + quy tắc nghiệp vụ thuần: `Link.Accessible()`, `Link.CountsViewFrom()`, sinh/validate mã, lỗi domain | Chỉ thư viện chuẩn |
| **usecase** | Điều phối nghiệp vụ (Create, Resolve, SetStatus, Delete, Stats) + **định nghĩa port** (`LinkRepository`, `LinkCache`, `ContentReader`, `ViewTracker`) | `domain` |
| **adapter** | Cài đặt port: PostgreSQL (`pgstore`), Redis (`redisstore`), in-memory (`memstore`), JWT (`token`); delivery HTTP (`httpapi`) | `usecase`, `domain`, framework |
| **infrastructure** | Config, kết nối/migration DB, kết nối Redis, logger | Mọi lớp |
| **cmd/api** | Composition root: khởi tạo & nối mọi implementation (DI thủ công) | Mọi lớp |

Dependency Rule được **kiểm tra bằng test** (`internal/archtest`): lớp trong import lớp ngoài hoặc framework → test fail.

```
cmd/api/main.go                     Composition root + graceful shutdown (drain view, flush lần cuối)
internal/
├── domain/                         link.go · code.go · viewer.go · submission.go · errors.go
├── usecase/
│   ├── share/
│   │   ├── ports.go                Output ports (interface) do use case định nghĩa
│   │   ├── dto.go                  CreateInput · SharedView · Stats
│   │   ├── create.go               Idempotent, retry khi trùng mã
│   │   ├── resolve.go              Validate → cache → singleflight → repo → quyền → track view
│   │   └── manage.go               SetStatus · Delete (invalidate cache) · Stats
│   └── submission/                 Xoá bài gốc → invalidate snapshot → link trả 410
├── adapter/
│   ├── httpapi/                    Router, handler, DTO, map lỗi domain → HTTP, trang /s/:code
│   │   └── middleware/             RequireAuth · OptionalAuth · RateLimit · RequestLogger
│   ├── pgstore/                    LinkRepository · SubmissionRepository (pgx)
│   ├── redisstore/                 LinkCache · CachedContentReader (decorator) · ViewCounter · RateLimiter
│   ├── memstore/                   Cài đặt in-memory mọi port → unit test không cần DB/Redis
│   └── token/                      JWT HS256 (ErrExpired → TOKEN_EXPIRED)
├── infrastructure/                 config · datastore (Postgres, Redis, migration) · logger
└── archtest/                       Test kiểm tra Dependency Rule
migrations/                         SQL nhúng vào binary (embed), chạy tự động khi khởi động
```

**Lợi ích cụ thể trong project này**
- Use case test bằng `memstore` (không Docker, chạy vài ms) — kể cả test 200 request đồng thời chỉ tạo 1 query DB.
- Cache snapshot là **decorator** (`CachedContentReader` bọc `SubmissionRepository`) — use case không biết có cache.
- Đổi Redis → Memcached, hay Gin → gRPC chỉ cần viết adapter mới, không sửa use case/domain.

## Quyết định kỹ thuật chính

- **Mã 8 ký tự Base62 từ `crypto/rand`**: 62⁸ ≈ 2,18 × 10¹⁴ tổ hợp, không đoán/dò tuần tự được (khác auto-increment hay Hashids). Trùng mã → `UNIQUE` constraint + retry.
- **Không dùng 301**: trình duyệt cache vĩnh viễn → tắt link không có hiệu lực. Endpoint public trả `Cache-Control: private, no-store`.
- **404 vs 410**: 404 khi mã không tồn tại; 410 khi đã tắt/xoá/hết hạn/bài gốc bị xoá. Người không phải chủ luôn nhận 404 → không lộ việc link tồn tại.
- **Không UPDATE từng view**: link viral sẽ tạo hot-row lock. Redis INCR + flush batch 1000 link/lần bằng `UPDATE ... FROM unnest()`.
- **Không tính** view của chủ bài, crawler preview (Facebook, Zalo…), và F5 lặp lại trong 30 phút.
- **Redis lỗi** → resolve fallback DB (có singleflight bảo vệ); rate limit fail-open.
- **Log dùng route pattern** (`/s/:code`) thay vì path thật để mã chia sẻ không lộ trong log.

## Production checklist

- Đặt `APP_ENV=production` (tắt `/api/v1/dev/token`, log JSON) và `JWT_SECRET` mạnh từ Secret Manager.
- Đặt `TRUSTED_PROXIES` (IP của Nginx/ALB) để rate limit dùng đúng IP người dùng.
- Next.js SSR gọi `GET /api/v1/shares/:code`, forward cookie ẩn danh qua header `X-Visitor-Id`.
- Redis Cluster: các key theo link đã dùng hash tag `{id}` nên Lua script đa key vẫn hợp lệ.

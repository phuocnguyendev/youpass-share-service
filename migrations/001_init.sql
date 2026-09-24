-- Bảng tối giản của các module có sẵn (users, submissions) để service chạy độc lập.
CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL    PRIMARY KEY,
    email         VARCHAR(255) NOT NULL UNIQUE,
    display_name  VARCHAR(100) NOT NULL,
    status        VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT chk_users_status CHECK (status IN ('active', 'locked'))
);

CREATE TABLE IF NOT EXISTS submissions (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     BIGINT       NOT NULL REFERENCES users(id),
    type        VARCHAR(32)  NOT NULL,
    title       VARCHAR(255) NOT NULL,
    content     TEXT         NOT NULL,
    band_score  REAL,
    feedback    TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT chk_submissions_type
        CHECK (type IN ('writing_submission', 'speaking_submission', 'test_result'))
);

CREATE INDEX IF NOT EXISTS idx_submissions_user ON submissions (user_id) WHERE deleted_at IS NULL;

-- Link chia sẻ bài làm: https://youpass.vn/s/{code}
CREATE TABLE IF NOT EXISTS share_links (
    id              BIGSERIAL    PRIMARY KEY,
    code            VARCHAR(12)  NOT NULL,                 -- không bao giờ tái sử dụng
    owner_id        BIGINT       NOT NULL REFERENCES users(id),
    resource_type   VARCHAR(32)  NOT NULL,
    resource_id     BIGINT       NOT NULL,
    status          VARCHAR(16)  NOT NULL DEFAULT 'active',-- active | disabled (tắt/bật lại được)
    expires_at      TIMESTAMPTZ,                           -- NULL = không hết hạn
    view_count      BIGINT       NOT NULL DEFAULT 0,       -- flush định kỳ từ Redis
    last_viewed_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,                           -- soft delete
    CONSTRAINT uq_share_links_code UNIQUE (code),
    CONSTRAINT chk_share_links_status CHECK (status IN ('active', 'disabled'))
);

-- Mỗi bài làm chỉ có 1 link còn sống → tạo lại là idempotent, chống race khi bấm "Chia sẻ" 2 lần
CREATE UNIQUE INDEX IF NOT EXISTS uq_share_links_active_resource
    ON share_links (resource_type, resource_id)
    WHERE deleted_at IS NULL;

-- Trang "Link đã chia sẻ của tôi"
CREATE INDEX IF NOT EXISTS idx_share_links_owner
    ON share_links (owner_id, created_at DESC)
    WHERE deleted_at IS NULL;

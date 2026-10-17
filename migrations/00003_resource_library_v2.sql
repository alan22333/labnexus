-- +goose Up
-- 资源库 v2:分类字典 + 置顶/计数 + 个人收藏(见 docs/specs/resource-library-v2.md)

CREATE TABLE resource_categories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(50) NOT NULL UNIQUE,
    sort       INT NOT NULL DEFAULT 0,
    builtin    BOOLEAN NOT NULL DEFAULT FALSE,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE resources
    ADD COLUMN category_id    UUID REFERENCES resource_categories(id) ON DELETE SET NULL,
    ADD COLUMN pinned         BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN view_count     BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN download_count BIGINT NOT NULL DEFAULT 0;

CREATE INDEX idx_resources_category ON resources(category_id);
CREATE INDEX idx_resources_pinned ON resources(pinned);

CREATE TABLE resource_favorites (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id UUID NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, resource_id)
);
CREATE INDEX idx_resource_favorites_resource ON resource_favorites(resource_id);

-- 内置分类种子(唯一键保证重放安全)
INSERT INTO resource_categories (id, name, sort, builtin, active)
VALUES
    (gen_random_uuid(), '文献', 1, TRUE, TRUE),
    (gen_random_uuid(), '数据集', 2, TRUE, TRUE),
    (gen_random_uuid(), '代码', 3, TRUE, TRUE),
    (gen_random_uuid(), '工具', 4, TRUE, TRUE),
    (gen_random_uuid(), '模板', 5, TRUE, TRUE),
    (gen_random_uuid(), '其他', 6, TRUE, TRUE)
ON CONFLICT (name) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS resource_favorites;
DROP INDEX IF EXISTS idx_resources_pinned;
DROP INDEX IF EXISTS idx_resources_category;
ALTER TABLE resources
    DROP COLUMN IF EXISTS download_count,
    DROP COLUMN IF EXISTS view_count,
    DROP COLUMN IF EXISTS pinned,
    DROP COLUMN IF EXISTS category_id;
DROP TABLE IF EXISTS resource_categories;

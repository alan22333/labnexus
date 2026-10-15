-- +goose Up
-- 内置任务类型种子。使用唯一键保证迁移重放安全。
INSERT INTO task_types (id, name, color, sort, builtin, active)
VALUES
    (gen_random_uuid(), '实验', '#3b82f6', 1, TRUE, TRUE),
    (gen_random_uuid(), '文献', '#8b5cf6', 2, TRUE, TRUE),
    (gen_random_uuid(), '写作', '#10b981', 3, TRUE, TRUE),
    (gen_random_uuid(), '代码', '#f59e0b', 4, TRUE, TRUE),
    (gen_random_uuid(), '其他', '#94a3b8', 5, TRUE, TRUE)
ON CONFLICT (name) DO NOTHING;

-- +goose Down
DELETE FROM task_types
WHERE builtin = TRUE
  AND name IN ('实验', '文献', '写作', '代码', '其他');

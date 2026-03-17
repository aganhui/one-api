-- ============================================================
-- One-API 批量插入渠道脚本
-- 生成自 ca_reqres/apis.txt + ca_reqres/models.txt
-- 渠道类型 type=1 (OpenAI 兼容)，状态 status=1 (启用)
-- 支持模型: gpt-5.4-medium, claude-4.6-opus-high-thinking, claude-4.6-sonnet-medium-thinking
-- 模型重定向:
--   gpt-5.4-medium                    -> glm-5-openclaw
--   claude-4.6-opus-high-thinking     -> deepseek-v3.1
--   claude-4.6-sonnet-medium-thinking -> ERNIE-5.0
-- 共 14 条渠道记录（每个 IP:Port 一条）
-- ============================================================

-- 若需重新导入，先取消注释下面两行清理旧数据：
-- DELETE FROM `abilities` WHERE channel_id IN (SELECT id FROM `channels` WHERE name LIKE 'CA-Node-%');
-- DELETE FROM `channels` WHERE name LIKE 'CA-Node-%';

-- ============================================================
-- STEP 1: 插入 channels 表
-- ============================================================

SET @key = 'eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJpc3N1c2VyIiwiYXVkIjoiYXVkaWVuY2UiLCJ0ZW5hbnRfaWQiOiI0MTI2NjMiLCJyb2xlX25hbWUiOiIiLCJ1c2VyX2lkIjoiMjAyMDA0NzE4NTU5MzMzOTkwNSIsInJvbGVfaWQiOiItMSIsInVzZXJfbmFtZSI6ImJpbmdhaHVpIiwib2F1dGhfaWQiOiIyMDIwMDQ2ODAwMjU0MjQyODE4IiwidG9rZW5fdHlwZSI6ImFjY2Vzc190b2tlbiIsImRlcHRfaWQiOiItMSIsImFjY291bnQiOiJiaW5nYWh1aSIsImNsaWVudF9pZCI6InNhYmVyIiwiZXhwIjoxNzc0MjYzNTI2LCJuYmYiOjE3NzM2NTg3MjZ9.5cszL93uCIMdqOCjeqCrSOsPJydHyUag6AXO9W9Qnso';
SET @mm  = '{"glm-5-openclaw":"gpt-5.4-medium","deepseek-v3.1":"claude-4.6-opus-high-thinking","ERNIE-5.0":"claude-4.6-sonnet-medium-thinking"}';
SET @mdl = 'gpt-5.4-medium,claude-4.6-opus-high-thinking,claude-4.6-sonnet-medium-thinking';

INSERT INTO `channels`
  (`type`,`key`,`status`,`name`,`weight`,`created_time`,`base_url`,`other`,`balance`,`balance_updated_time`,`models`,`group`,`used_quota`,`model_mapping`,`priority`,`config`,`system_prompt`)
VALUES
  (1,@key,1,'CA-Node-01 (43.242.200.6:3001)',  10,UNIX_TIMESTAMP(),'http://43.242.200.6:3001',  NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-02 (43.242.200.8:3001)',  10,UNIX_TIMESTAMP(),'http://43.242.200.8:3001',  NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-03 (43.242.200.119:3001)',10,UNIX_TIMESTAMP(),'http://43.242.200.119:3001',NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-04 (43.242.200.48:3001)', 10,UNIX_TIMESTAMP(),'http://43.242.200.48:3001', NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-05 (149.88.92.191:3001)', 10,UNIX_TIMESTAMP(),'http://149.88.92.191:3001', NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-06 (149.88.92.227:3001)', 10,UNIX_TIMESTAMP(),'http://149.88.92.227:3001', NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-07 (43.242.200.55:3001)', 10,UNIX_TIMESTAMP(),'http://43.242.200.55:3001', NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-08 (154.37.218.149:3001)',10,UNIX_TIMESTAMP(),'http://154.37.218.149:3001',NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-09 (154.37.218.149:3000)',10,UNIX_TIMESTAMP(),'http://154.37.218.149:3000',NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-10 (154.37.218.131:3001)',10,UNIX_TIMESTAMP(),'http://154.37.218.131:3001',NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-11 (154.37.218.131:3000)',10,UNIX_TIMESTAMP(),'http://154.37.218.131:3000',NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-12 (154.44.31.38:3000)',  10,UNIX_TIMESTAMP(),'http://154.44.31.38:3000',  NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-13 (154.44.31.38:3001)',  10,UNIX_TIMESTAMP(),'http://154.44.31.38:3001',  NULL,0,0,@mdl,'default',0,@mm,0,'',NULL),
  (1,@key,1,'CA-Node-14 (154.44.31.38:3002)',  10,UNIX_TIMESTAMP(),'http://154.44.31.38:3002',  NULL,0,0,@mdl,'default',0,@mm,0,'',NULL);

-- ============================================================
-- STEP 2: 同步写入 abilities 表
-- 路由分发依赖此表，缺失会导致「无可用渠道」
-- 14 渠道 x 3 模型 = 42 条记录
-- ============================================================

INSERT INTO `abilities` (`group`,`model`,`channel_id`,`enabled`,`priority`)
SELECT
  c.`group`,
  m.model,
  c.id,
  1,
  COALESCE(c.priority, 0)
FROM `channels` c
JOIN (
  SELECT 'gpt-5.4-medium'                   AS model UNION ALL
  SELECT 'claude-4.6-opus-high-thinking'     AS model UNION ALL
  SELECT 'claude-4.6-sonnet-medium-thinking' AS model
) m ON FIND_IN_SET(m.model, c.models) > 0
WHERE c.name LIKE 'CA-Node-%'
ON DUPLICATE KEY UPDATE
  enabled  = VALUES(enabled),
  priority = VALUES(priority);

-- ============================================================
-- STEP 3: 补充更新 model_mapping（适用于之前已导入但未设映射的渠道）
-- ============================================================

UPDATE `channels`
SET `model_mapping` = @mm
WHERE name LIKE 'CA-Node-%'
  AND (`model_mapping` IS NULL OR `model_mapping` = '' OR `model_mapping` = '{}');

-- ============================================================
-- 完成！刷新 One-API 管理后台页面即可看到 14 个渠道。
--
-- SQLite 用户注意：
--   UNIX_TIMESTAMP()      -> strftime('%s','now')
--   FIND_IN_SET(x,y) > 0  -> instr(c.models, m.model) > 0
--   ON DUPLICATE KEY UPDATE -> INSERT OR REPLACE INTO
-- ============================================================

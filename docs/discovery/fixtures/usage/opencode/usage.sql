-- Fixture de consumo do OpenCode: esquema de schema-v1.sql + 3 respostas com tokens (números reais, texto fictício).
CREATE TABLE "__drizzle_migrations" (id INTEGER PRIMARY KEY, hash text NOT NULL, created_at numeric, name text, applied_at TEXT);
INSERT INTO "__drizzle_migrations" (id, hash, created_at, name) VALUES (13, 'fixture', 1776000000000, '20260423070820_add_icon_url_override');

CREATE TABLE `project` (`id` text PRIMARY KEY, `worktree` text NOT NULL, `vcs` text, `name` text, `icon_url` text, `icon_color` text,
  `time_created` integer NOT NULL, `time_updated` integer NOT NULL, `time_initialized` integer, `sandboxes` text NOT NULL, `commands` text, `icon_url_override` text);
CREATE TABLE `session` (`id` text PRIMARY KEY, `project_id` text NOT NULL, `parent_id` text, `slug` text NOT NULL, `directory` text NOT NULL,
  `title` text NOT NULL, `version` text NOT NULL, `share_url` text, `summary_additions` integer, `summary_deletions` integer, `summary_files` integer,
  `summary_diffs` text, `revert` text, `permission` text, `time_created` integer NOT NULL, `time_updated` integer NOT NULL,
  `time_compacting` integer, `time_archived` integer, `workspace_id` text);
CREATE TABLE `message` (`id` text PRIMARY KEY, `session_id` text NOT NULL, `time_created` integer NOT NULL, `time_updated` integer NOT NULL, `data` text NOT NULL);
CREATE TABLE `part` (`id` text PRIMARY KEY, `message_id` text NOT NULL, `session_id` text NOT NULL, `time_created` integer NOT NULL, `time_updated` integer NOT NULL, `data` text NOT NULL);

INSERT INTO project VALUES ('1111111111111111111111111111111111111111', '/home/user/projects/demo-app', 'git', NULL, NULL, NULL, 1795622400000, 1795622400000, 1795622400000, '[]', NULL, NULL);
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, summary_additions, summary_deletions, summary_files, time_created, time_updated)
  VALUES ('ses_usage00001AAAAAAAAAAAAAAAA', '1111111111111111111111111111111111111111', NULL, 'usage-fixture', '/home/user/projects/demo-app', 'Ajustar build', '1.14.24', 0, 0, 0, 1795622400000, 1795622460000);
INSERT INTO message VALUES ('msg_usage00001AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622401000, 1795622401000,
  '{"role":"user","time":{"created":1795622401000},"agent":"build","model":{"providerID":"anthropic","modelID":"claude-opus-5-5"}}');
INSERT INTO part VALUES ('prt_usage00001AAAAAAAAAAAAAAAA', 'msg_usage00001AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622401000, 1795622401000, '{"type":"text","text":"Ajuste o build"}');
-- 1) total informado
INSERT INTO message VALUES ('msg_usage00002AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622402000, 1795622410000,
  '{"role":"assistant","time":{"created":1795622402000,"completed":1795622410000},"parentID":"msg_usage00001AAAAAAAAAAAAAAAA","modelID":"claude-opus-4.6","providerID":"github-copilot","mode":"build","cost":0,"tokens":{"total":11726,"input":11440,"output":286,"reasoning":0,"cache":{"read":0,"write":0}},"finish":"tool-calls"}');
INSERT INTO part VALUES ('prt_usage00002AAAAAAAAAAAAAAAA', 'msg_usage00002AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622403000, 1795622403000, '{"type":"text","text":"Vou ajustar."}');
INSERT INTO part VALUES ('prt_usage00003AAAAAAAAAAAAAAAA', 'msg_usage00002AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622409000, 1795622409000,
  '{"type":"step-finish","reason":"tool-calls","snapshot":"0000000000000000000000000000000000000000","cost":0,"tokens":{"total":5465,"input":5059,"output":406,"reasoning":0,"cache":{"read":0,"write":0}}}');
-- 2) total ausente, reasoning 0: derivável
INSERT INTO message VALUES ('msg_usage00003AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622420000, 1795622430000,
  '{"role":"assistant","time":{"created":1795622420000,"completed":1795622430000},"parentID":"msg_usage00001AAAAAAAAAAAAAAAA","modelID":"claude-opus-4.6","providerID":"github-copilot","mode":"build","cost":0,"tokens":{"input":5059,"output":406,"reasoning":0,"cache":{"read":100,"write":50}},"finish":"stop"}');
INSERT INTO part VALUES ('prt_usage00004AAAAAAAAAAAAAAAA', 'msg_usage00003AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622421000, 1795622421000, '{"type":"text","text":"Build ajustado."}');
-- 3) total ausente, reasoning > 0: total indisponível
INSERT INTO message VALUES ('msg_usage00004AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622450000, 1795622460000,
  '{"role":"assistant","time":{"created":1795622450000,"completed":1795622460000},"parentID":"msg_usage00001AAAAAAAAAAAAAAAA","modelID":"gpt-5.4","providerID":"openai","mode":"build","cost":0,"tokens":{"input":300,"output":80,"reasoning":12,"cache":{"read":0,"write":0}},"finish":"stop"}');
INSERT INTO part VALUES ('prt_usage00005AAAAAAAAAAAAAAAA', 'msg_usage00004AAAAAAAAAAAAAAAA', 'ses_usage00001AAAAAAAAAAAAAAAA', 1795622451000, 1795622451000, '{"type":"text","text":"Feito."}');

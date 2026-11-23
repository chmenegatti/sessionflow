-- Fixture sintético do esquema SQLite do OpenCode v1.x (observado na 1.14.24).
-- Apenas tabelas lidas pelo SessionFlow. Tabelas com credenciais (account, control_account) omitidas de propósito.
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

INSERT INTO project VALUES ('0123456789abcdef0123456789abcdef01234567', '/home/user/projects/demo-app', 'git', NULL, NULL, NULL, 1768046400000, 1768046405000, 1768046400000, '[]', NULL, NULL);
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, summary_additions, summary_deletions, summary_files, time_created, time_updated)
  VALUES ('ses_fixture000001AAAAAAAAAAAAAA', '0123456789abcdef0123456789abcdef01234567', NULL, 'quiet-fixture', '/home/user/projects/demo-app', 'Listar arquivos Go', '1.14.24', 0, 0, 0, 1768046400000, 1768046405000);
INSERT INTO message VALUES ('msg_fixture000001AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046401000, 1768046401000,
  '{"role":"user","time":{"created":1768046401000},"agent":"build","model":{"providerID":"anthropic","modelID":"claude-opus-5-5"}}');
INSERT INTO message VALUES ('msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046402000, 1768046405000,
  '{"role":"assistant","time":{"created":1768046402000,"completed":1768046405000},"parentID":"msg_fixture000001AAAAAAAAAAAAAA","modelID":"claude-opus-5-5","providerID":"anthropic","mode":"build","agent":"build","path":{"cwd":"/home/user/projects/demo-app","root":"/home/user/projects/demo-app"},"cost":0,"tokens":{"total":120,"input":100,"output":20,"reasoning":0,"cache":{"read":0,"write":0}},"finish":"stop"}');
INSERT INTO part VALUES ('prt_fixture000001AAAAAAAAAAAAAA', 'msg_fixture000001AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046401000, 1768046401000,
  '{"type":"text","text":"Liste os arquivos Go do projeto"}');
INSERT INTO part VALUES ('prt_fixture000002AAAAAAAAAAAAAA', 'msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046402000, 1768046402000,
  '{"type":"step-start","snapshot":"0000000000000000000000000000000000000000"}');
INSERT INTO part VALUES ('prt_fixture000003AAAAAAAAAAAAAA', 'msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046402500, 1768046402500,
  '{"type":"reasoning","text":"Preciso listar arquivos .go","time":{"start":1768046402500,"end":1768046402600}}');
INSERT INTO part VALUES ('prt_fixture000004AAAAAAAAAAAAAA', 'msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046403000, 1768046404000,
  '{"type":"tool","callID":"toolu_fixture0001","tool":"bash","state":{"status":"completed","input":{"command":"find . -name ''*.go''","description":"List Go files"},"output":"./main.go\n./app.go","title":"List Go files","metadata":{},"time":{"start":1768046403000,"end":1768046404000}}}');
INSERT INTO part VALUES ('prt_fixture000005AAAAAAAAAAAAAA', 'msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046404500, 1768046404500,
  '{"type":"text","text":"Encontrei 2 arquivos: main.go e app.go."}');
INSERT INTO part VALUES ('prt_fixture000006AAAAAAAAAAAAAA', 'msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046405000, 1768046405000,
  '{"type":"step-finish","reason":"stop","snapshot":"0000000000000000000000000000000000000000","cost":0,"tokens":{"total":120,"input":100,"output":20,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO part VALUES ('prt_fixture000007AAAAAAAAAAAAAA', 'msg_fixture000002AAAAAAAAAAAAAA', 'ses_fixture000001AAAAAAAAAAAAAA', 1768046405100, 1768046405100,
  '{"type":"future-unknown-part","x":1}');

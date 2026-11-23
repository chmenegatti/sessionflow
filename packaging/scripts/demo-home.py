#!/usr/bin/env python3
"""Cria um HOME de demonstração com históricos sintéticos dos três agentes,
nos formatos dos fixtures de docs/discovery/fixtures, para capturas de tela
(README, site). Nada vem de conversas reais. Datas relativas a hoje, então o
Dashboard mostra atividade nos últimos 30 dias.

Uso: demo-home.py DESTINO
Depois: HOME=DESTINO XDG_DATA_HOME=DESTINO/.local/share ... sessionflow
"""
import hashlib
import json
import os
import random
import sqlite3
import sys
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCHEMA = ROOT / "docs/discovery/fixtures/opencode/schema-v1.sql"

PROJECTS = ["api-gateway", "checkout-web", "infra-terraform", "mobile-app", "data-pipeline", "design-system"]
TOOLS_CLAUDE = ["Bash"] * 6 + ["Read"] * 4 + ["Edit"] * 3 + ["Write", "Grep", "Glob"]
TITLES = [
    "Refatorar cliente HTTP com retry", "Corrigir teste instável de pagamento", "Migrar pipeline para Go 1.26",
    "Adicionar cache no endpoint de produtos", "Revisar política de IAM", "Criar componente de tabela virtual",
    "Investigar vazamento de memória no worker", "Documentar fluxo de autenticação", "Paginação por cursor na API",
    "Ajustar tema escuro dos gráficos", "Atualizar dependências e changelog", "Script de migração de dados",
]
PROMPTS = ["Pode olhar isso?", "Rode os testes de novo", "Agora ajuste o README", "Explique a diferença", "Faça o commit"]


def iso(t: datetime) -> str:
    return t.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"


def write(path: Path, records, last: datetime):
    # mtime = último registro: "aberta agora" usa a alteração recente do arquivo.
    path.write_text("".join(json.dumps(x, ensure_ascii=False) + "\n" for x in records))
    os.utime(path, (last.timestamp(), last.timestamp()))


def claude_usage(rnd: random.Random):
    # Consumo sintético, nas proporções vistas em sessões reais (cache domina).
    return {"input_tokens": rnd.randint(1, 8), "output_tokens": rnd.randint(40, 900),
            "cache_read_input_tokens": rnd.randint(20_000, 180_000), "cache_creation_input_tokens": rnd.randint(200, 6_000)}


def claude_session(home: Path, rnd: random.Random, project: str, start: datetime, turns: int):
    sid = str(uuid.UUID(int=rnd.getrandbits(128), version=4))
    cwd = f"{home}/projects/{project}"
    d = home / ".claude/projects" / cwd.replace("/", "-")
    d.mkdir(parents=True, exist_ok=True)
    base = {"isSidechain": False, "userType": "external", "entrypoint": "cli", "cwd": cwd, "sessionId": sid, "version": "2.1.330", "gitBranch": "main"}
    lines, parent, t = [], None, start
    title = rnd.choice(TITLES)

    def add(kind, message, **extra):
        nonlocal parent, t
        u = str(uuid.UUID(int=rnd.getrandbits(128), version=4))
        t += timedelta(seconds=rnd.randint(4, 90))
        lines.append({**base, "parentUuid": parent, "type": kind, "message": message, "uuid": u, "timestamp": iso(t), **extra})
        parent = u

    for i in range(turns):
        add("user", {"role": "user", "content": title if i == 0 else rnd.choice(PROMPTS)})
        for _ in range(rnd.randint(1, 3)):
            tool = rnd.choice(TOOLS_CLAUDE)
            tid = f"toolu_{rnd.getrandbits(64):016x}"
            add("assistant", {"model": "claude-opus-5-5", "role": "assistant", "type": "message", "id": f"msg_{rnd.getrandbits(64):016x}",
                              "content": [{"type": "tool_use", "id": tid, "name": tool, "input": {"command": "go test ./..."}}],
                              "usage": claude_usage(rnd)})
            add("user", {"role": "user", "content": [{"type": "tool_result", "tool_use_id": tid, "content": "ok", "is_error": False}]})
        add("assistant", {"model": "claude-opus-5-5", "role": "assistant", "type": "message", "id": f"msg_{rnd.getrandbits(64):016x}",
                          "content": [{"type": "text", "text": "Feito. Os testes passam:\n\n```\nok  \tdemo/internal/http\t0.412s\n```"}],
                          "usage": claude_usage(rnd)})
    lines.append({"type": "ai-title", "aiTitle": title, "sessionId": sid})
    write(d / f"{sid}.jsonl", lines, t)


def codex_session(home: Path, rnd: random.Random, project: str, start: datetime, turns: int):
    sid = str(uuid.UUID(int=rnd.getrandbits(128), version=7))
    cwd = f"{home}/projects/{project}"
    d = home / ".codex/sessions" / start.strftime("%Y/%m/%d")
    d.mkdir(parents=True, exist_ok=True)
    t = start
    out = [{"timestamp": iso(t), "type": "session_meta", "payload": {"id": sid, "timestamp": iso(t), "cwd": cwd, "originator": "codex_cli_rs",
                                                                      "cli_version": "0.156.1", "source": "cli", "model_provider": "openai"}},
           {"timestamp": iso(t), "type": "turn_context", "payload": {"turn_id": "t0", "cwd": cwd, "model": "gpt-5.4", "summary": "auto"}}]
    total = {"input_tokens": 0, "cached_input_tokens": 0, "output_tokens": 0, "reasoning_output_tokens": 0, "total_tokens": 0}

    def item(payload):
        nonlocal t
        t += timedelta(seconds=rnd.randint(4, 60))
        out.append({"timestamp": iso(t), "type": "response_item", "payload": payload})

    for i in range(turns):
        text = rnd.choice(TITLES) if i == 0 else rnd.choice(PROMPTS)
        item({"type": "message", "role": "user", "content": [{"type": "input_text", "text": text}]})
        out.append({"timestamp": iso(t), "type": "event_msg", "payload": {"type": "user_message", "message": text, "images": []}})
        cid = f"call_{rnd.getrandbits(48):012x}"
        item({"type": "function_call", "name": "exec_command", "arguments": json.dumps({"cmd": "make test"}), "call_id": cid})
        item({"type": "function_call_output", "call_id": cid, "output": "PASS"})
        item({"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Pronto, tudo verde."}]})
        inp, cached, outp = rnd.randint(8_000, 60_000), 0, rnd.randint(150, 900)
        cached = int(inp * rnd.uniform(0.5, 0.95))
        last = {"input_tokens": inp, "cached_input_tokens": cached, "output_tokens": outp,
                "reasoning_output_tokens": rnd.randint(0, outp // 3), "total_tokens": inp + outp}
        for k in total:
            total[k] += last[k]
        out.append({"timestamp": iso(t), "type": "event_msg", "payload": {"type": "token_count",
                    "info": {"last_token_usage": last, "total_token_usage": dict(total), "model_context_window": 258400}}})
    name = f"rollout-{start.strftime('%Y-%m-%dT%H-%M-%S')}-{sid}.jsonl"
    write(d / name, out, t)


def opencode_sessions(home: Path, rnd: random.Random, plan):
    d = home / ".local/share/opencode"
    d.mkdir(parents=True, exist_ok=True)
    db = sqlite3.connect(d / "opencode.db")
    db.executescript(SCHEMA.read_text())
    ms = lambda t: int(t.timestamp() * 1000)
    for n, (project, start, turns) in enumerate(plan):
        cwd = f"{home}/projects/{project}"
        pid = hashlib.sha1(project.encode()).hexdigest()
        db.execute("INSERT OR IGNORE INTO project VALUES (?, ?, 'git', NULL, NULL, NULL, ?, ?, ?, '[]', NULL, NULL)", (pid, cwd, ms(start), ms(start), ms(start)))
        sid = f"ses_demo{n:06d}AAAAAAAAAAAAAAAA"
        t = start
        for i in range(turns):
            for role in ("user", "assistant"):
                t += timedelta(seconds=rnd.randint(5, 80))
                mid = f"msg_demo{n:04d}{i:03d}{role[0]}AAAAAAAAAAAAA"
                data = {"role": role, "time": {"created": ms(t)}, "modelID": "claude-opus-5-5", "providerID": "anthropic"}
                if role == "assistant":
                    tin, tout, cr = rnd.randint(2_000, 30_000), rnd.randint(80, 700), rnd.randint(0, 20_000)
                    data["tokens"] = {"total": tin + tout + cr, "input": tin, "output": tout, "reasoning": 0, "cache": {"read": cr, "write": 0}}
                db.execute("INSERT INTO message VALUES (?, ?, ?, ?, ?)", (mid, sid, ms(t), ms(t), json.dumps(data)))
                text = (rnd.choice(TITLES) if i == 0 else rnd.choice(PROMPTS)) if role == "user" else "Ajustei os arquivos."
                db.execute("INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)", (f"prt_{mid[4:]}t", mid, sid, ms(t), ms(t), json.dumps({"type": "text", "text": text})))
                if role == "assistant":
                    tool = {"type": "tool", "callID": f"call_{n}_{i}", "tool": rnd.choice(["bash", "read", "edit"]),
                            "state": {"status": "completed", "input": {"command": "npm test"}, "output": "ok", "time": {"start": ms(t), "end": ms(t)}}}
                    db.execute("INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)", (f"prt_{mid[4:]}x", mid, sid, ms(t), ms(t), json.dumps(tool)))
        db.execute("""INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, summary_additions, summary_deletions,
                      summary_files, time_created, time_updated) VALUES (?, ?, NULL, ?, ?, ?, '1.14.24', 0, 0, 0, ?, ?)""",
                   (sid, pid, f"demo-{n}", cwd, rnd.choice(TITLES), ms(start), ms(t)))
    db.commit()
    db.close()


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    home = Path(sys.argv[1]).resolve()
    rnd = random.Random(42)
    now = datetime.now().astimezone().replace(minute=0, second=0, microsecond=0)
    for p in PROJECTS:
        (home / "projects" / p).mkdir(parents=True, exist_ok=True)
    oc = []
    # Mais atividade perto de hoje; alguns dias sem nada.
    for day in range(75, -1, -1):
        if rnd.random() < 0.25:
            continue
        for _ in range(rnd.choice([1, 1, 2, 2, 3]) + (1 if day < 14 else 0)):
            start = now - timedelta(days=day, hours=rnd.randint(1, 10))
            project = rnd.choices(PROJECTS, weights=[6, 5, 3, 2, 2, 1])[0]
            turns = rnd.randint(2, 9)
            r = rnd.random()
            if r < 0.62:
                claude_session(home, rnd, project, start, turns)
            elif r < 0.84:
                codex_session(home, rnd, project, start, turns)
            else:
                oc.append((project, start, turns))
    opencode_sessions(home, rnd, oc)
    print(home)


if __name__ == "__main__":
    main()

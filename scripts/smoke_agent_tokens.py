#!/usr/bin/env python3
"""Smoke test end-to-end de tokens por agente y agent_role.

Arranca un server temporal, usa una config temporal y verifica:
login, init, project, agent ensure, me, ticket, audit, rotate, revoke,
re-enrolamiento, MCP y baja.
"""

from __future__ import annotations

import json
import os
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
BIN = REPO / "bin" / "agentbank"
HUMAN_TOKEN = "ab_smoke_agent_tokens_0123456789abcdef"


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def http_status(url: str, token: str) -> int:
    request = urllib.request.Request(url, headers={"Authorization": "Bearer " + token})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return int(response.status)
    except urllib.error.HTTPError as exc:
        return int(exc.code)
    except Exception:
        return 0


def wait_health(url: str) -> bool:
    deadline = time.time() + 15
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url + "/healthz", timeout=1) as response:
                if response.status == 200:
                    return True
        except Exception:
            pass
        time.sleep(0.2)
    return False


def main() -> int:
    if not BIN.is_file():
        print(f"error: falta {BIN}; corré `go build -o bin/agentbank ./cmd/agentbank`", file=sys.stderr)
        return 1
    base = Path(os.environ.get("SMOKE_DIR", "/tmp/opencode"))
    base.mkdir(parents=True, exist_ok=True)
    tmp = Path(tempfile.mkdtemp(prefix="agentbank-smoke-", dir=base))
    work = tmp / "work"
    config = tmp / "config"
    work.mkdir()
    config.mkdir()
    port = free_port()
    url = f"http://127.0.0.1:{port}"
    db = tmp / "smoke.db"
    server_log = tmp / "server.log"
    env_base = os.environ.copy()
    env_base.update({
        "XDG_CONFIG_HOME": str(config),
        "AGENTBANK_URL": url,
        "AGENTBANK_TOKEN": HUMAN_TOKEN,
        "AGENTBANK_AGENT": "",
        "AGENTBANK_AGENT_ROLE": "",
        "AGENTBANK_PROJECT": "",
    })
    server = subprocess.Popen(
        [str(BIN), "serve"],
        cwd=str(work),
        env={
            **env_base,
            "AGENTBANK_ADDR": f"127.0.0.1:{port}",
            "AGENTBANK_DB": str(db),
            "AGENTBANK_BOOTSTRAP_TOKEN": HUMAN_TOKEN,
        },
        stdout=server_log.open("wb"),
        stderr=subprocess.STDOUT,
    )
    ok = False
    try:
        if not wait_health(url):
            print("error: el server no respondió /healthz", file=sys.stderr)
            return 1

        def run(*args: str, agent: str = "", token: str = "", check: bool = True) -> subprocess.CompletedProcess:
            env = env_base.copy()
            if agent:
                env["AGENTBANK_AGENT"] = agent
            if token:
                env["AGENTBANK_TOKEN"] = token
            proc = subprocess.run(
                [str(BIN), *args], cwd=str(work), env=env,
                capture_output=True, text=True,
            )
            if check and proc.returncode != 0:
                raise RuntimeError(
                    f"falló agentbank {' '.join(args)} (exit {proc.returncode}):\n"
                    f"stdout={proc.stdout}\nstderr={proc.stderr}"
                )
            return proc

        def data(*args: str, agent: str = "", token: str = "") -> dict:
            proc = run(*args, agent=agent, token=token)
            return json.loads(proc.stdout)

        run("login", "--url", url, "--project", "demo", "--token", HUMAN_TOKEN)
        run("init", "--url", url, "--project", "demo", "--agent", "auditor-smoke", "--role", "auditor", "--token", HUMAN_TOKEN)
        project = data("project", "create", "--slug", "demo", "--name", "Demo")
        if project.get("slug") != "demo":
            raise AssertionError(f"proyecto inesperado: {project}")

        ensure = data("agent", "ensure", "--agent", "auditor-smoke", "--role", "auditor", "--project", "demo")
        if not ensure.get("created"):
            raise AssertionError(f"el agente no se creó: {ensure}")

        token_path = config / "agentbank" / "tokens" / "auditor-smoke.token"
        human_path = config / "agentbank" / "human.token"
        for path in (token_path, human_path):
            if not path.is_file():
                raise AssertionError(f"falta {path}")
            if stat.S_IMODE(path.stat().st_mode) != 0o600:
                raise AssertionError(f"permisos inesperados en {path}: {oct(stat.S_IMODE(path.stat().st_mode))}")

        me = data("me", agent="auditor-smoke")
        if me["participant"].get("agent_role") != "auditor":
            raise AssertionError(f"agent_role inesperado: {me}")

        ticket = data(
            "api", "--method", "POST", "--path", "/v1/projects/demo/tickets",
            "--body", json.dumps({"title": "Ticket del auditor smoke"}),
            agent="auditor-smoke",
        )
        if ticket.get("created_by_role") != "auditor":
            raise AssertionError(f"created_by_role inesperado: {ticket}")
        if not ticket.get("created_by"):
            raise AssertionError(f"ticket sin autor: {ticket}")

        audit = data("audit", "list", "--chain", project["id"], "--limit", "50")
        events = audit.get("items", [])
        found = any(
            event.get("action") == "ticket.crear"
            and event.get("object_id") == ticket.get("id")
            and event.get("actor_role") == "auditor"
            for event in events
        )
        if not found:
            raise AssertionError(f"no se encontró actor_role=auditor en auditoría: {events}")

        old_token = token_path.read_text().strip()
        data("agent", "rotate", "--agent", "auditor-smoke")
        new_token = token_path.read_text().strip()
        if old_token == new_token:
            raise AssertionError("la rotación no cambió el token")
        if http_status(url + "/v1/me", old_token) != 401:
            raise AssertionError("el token viejo seguía válido")
        if http_status(url + "/v1/me", new_token) != 200:
            raise AssertionError("el token nuevo no funciona")

        data("agent", "revoke", "--agent", "auditor-smoke")
        if http_status(url + "/v1/me", new_token) != 401:
            raise AssertionError("el token revocado seguía válido")
        ensure_again = data("agent", "ensure", "--agent", "auditor-smoke", "--role", "auditor", "--project", "demo")
        if not ensure_again.get("rotated"):
            raise AssertionError(f"el re-enrolamiento no rotó: {ensure_again}")
        if http_status(url + "/v1/me", token_path.read_text().strip()) != 200:
            raise AssertionError("el token re-enrolado no funciona")

        mcp_input = "\n".join([
            json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}),
            json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
            json.dumps({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "me", "arguments": {}}}),
        ]) + "\n"
        mcp_env = env_base.copy()
        mcp_env["AGENTBANK_AGENT"] = "auditor-smoke"
        mcp = subprocess.run(
            [str(BIN), "mcp"], cwd=str(work), env=mcp_env,
            input=mcp_input, capture_output=True, text=True, timeout=30,
        )
        if mcp.returncode != 0:
            raise AssertionError(f"MCP falló: {mcp.stderr}")
        responses = [json.loads(line) for line in mcp.stdout.splitlines() if line.strip()]
        if len(responses) != 3:
            raise AssertionError(f"respuestas MCP inesperadas: {responses}")
        tools = responses[1]["result"]["tools"]
        if len(tools) < 70:
            raise AssertionError(f"tools MCP={len(tools)}")
        if "auditor" not in responses[2]["result"]["content"][0]["text"]:
            raise AssertionError(f"MCP me sin agent_role: {responses[2]}")

        data("agent", "disable", "--agent", "auditor-smoke")
        disabled = run("agent", "ensure", "--agent", "auditor-smoke", "--role", "auditor", "--project", "demo", check=False)
        if disabled.returncode == 0 or "baja" not in (disabled.stdout + disabled.stderr):
            raise AssertionError(f"se pudo re-enrolar un agente de baja: {disabled.stdout} {disabled.stderr}")

        print("OK: smoke de tokens por agente y agent_role")
        print(f"  run dir: {tmp}")
        ok = True
        return 0
    except Exception as exc:
        print(f"FALLO: {exc}", file=sys.stderr)
        print(f"  run dir: {tmp}", file=sys.stderr)
        print(f"  server log: {server_log}", file=sys.stderr)
        return 1
    finally:
        server.terminate()
        try:
            server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait(timeout=5)
        if ok and os.environ.get("SMOKE_KEEP") != "1":
            shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Configura el MCP de AgentBank en OpenCode y enrola un agente.

Uso:
  AGENTBANK_TOKEN='ab_...' scripts/setup-opencode-mcp.py \
    --project API --agent opencode-main --role orquestador

El token humano se guarda en ~/.config/agentbank/human.token.
El token del agente se guarda en ~/.config/agentbank/tokens/<agente>.token.
En ~/.config/opencode/opencode.json solo se escribe AGENTBANK_AGENT, no tokens.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path


def agentbank_bin() -> str:
    explicit = os.environ.get("AGENTBANK_BIN")
    if explicit:
        return explicit
    repo_bin = Path(__file__).resolve().parent.parent / "bin" / "agentbank"
    if repo_bin.is_file():
        return str(repo_bin)
    found = shutil.which("agentbank")
    if found:
        return found
    raise SystemExit("no encontré agentbank; compilá con `go build -o bin/agentbank ./cmd/agentbank`")


def opencode_config_path() -> Path:
    override = os.environ.get("OPENCODE_CONFIG")
    if override:
        return Path(override).expanduser()
    return Path.home() / ".config" / "opencode" / "opencode.json"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default=os.environ.get("AGENTBANK_URL", "http://127.0.0.1:8090"))
    parser.add_argument("--project", required=True)
    parser.add_argument("--agent", default="opencode-main")
    parser.add_argument("--role", default="orquestador")
    parser.add_argument("--token", default=os.environ.get("AGENTBANK_TOKEN", ""))
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    binary = agentbank_bin()
    env = os.environ.copy()
    if args.token:
        env["AGENTBANK_TOKEN"] = args.token
    if not env.get("AGENTBANK_TOKEN"):
        return _fail("falta AGENTBANK_TOKEN o --token")

    login = [
        binary, "login", "--url", args.url, "--project", args.project,
        "--token", env["AGENTBANK_TOKEN"],
    ]
    if subprocess.run(login, env=env).returncode != 0:
        return _fail("falló agentbank login")

    ensure = [
        binary, "agent", "ensure", "--agent", args.agent,
        "--role", args.role, "--project", args.project,
    ]
    if subprocess.run(ensure, env=env).returncode != 0:
        return _fail("falló agentbank agent ensure")

    path = opencode_config_path()
    config: dict = {}
    if path.is_file():
        try:
            config = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            return _fail(f"opencode.json inválido: {exc}")
    mcp = config.setdefault("mcp", {})
    mcp["agentbank"] = {
        "type": "local",
        "command": [binary, "mcp"],
        "environment": {
            "AGENTBANK_URL": args.url,
            "AGENTBANK_PROJECT": args.project,
            "AGENTBANK_AGENT": args.agent,
            "AGENTBANK_AGENT_ROLE": args.role,
        },
    }
    rendered = json.dumps(config, indent=2, ensure_ascii=False) + "\n"
    if args.dry_run:
        print(rendered)
        return 0
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_file():
        backup = path.with_suffix(path.suffix + ".bak")
        shutil.copy2(path, backup)
    path.write_text(rendered, encoding="utf-8")
    print(f"OK: MCP configurado en {path}")
    print(f"Agente: {args.agent} ({args.role}), proyecto {args.project}")
    print("Reiniciá OpenCode para que cargue el MCP.")
    return 0


def _fail(message: str) -> int:
    print(f"error: {message}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())

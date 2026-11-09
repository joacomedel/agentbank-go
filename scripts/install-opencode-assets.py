#!/usr/bin/env python3
"""Instala los agentes y skills de AgentBank en OpenCode.

Copia `opencode/agents/*.md` y `opencode/skills/*/SKILL.md` al directorio
global de OpenCode (`~/.config/opencode`). No toca tokens ni configuración
MCP: para eso está `scripts/setup-opencode-mcp.py`.

Uso:
  scripts/install-opencode-assets.py --dry-run
  scripts/install-opencode-assets.py
  scripts/install-opencode-assets.py --dest /ruta/a/.config/opencode
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SOURCE_AGENTS = REPO / "opencode" / "agents"
SOURCE_SKILLS = REPO / "opencode" / "skills"


def default_dest() -> Path:
    override = os.environ.get("OPENCODE_CONFIG_DIR")
    if override:
        return Path(override).expanduser()
    return Path.home() / ".config" / "opencode"


def install_file(source: Path, target: Path, dry_run: bool) -> str:
    if target.is_file() and target.read_bytes() == source.read_bytes():
        return f"sin cambios  {target}"
    if dry_run:
        return f"copiaría     {target}"
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.is_file():
        backup = target.with_suffix(target.suffix + ".bak")
        shutil.copy2(target, backup)
    shutil.copy2(source, target)
    return f"instalado    {target}"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dest", type=Path, default=default_dest())
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    if not SOURCE_AGENTS.is_dir() or not SOURCE_SKILLS.is_dir():
        print("error: faltan opencode/agents u opencode/skills", file=sys.stderr)
        return 1

    dest = args.dest.expanduser().resolve()
    print(f"destino: {dest}")
    if not args.dry_run:
        dest.mkdir(parents=True, exist_ok=True)

    changes = 0
    for source in sorted(SOURCE_AGENTS.glob("*.md")):
        message = install_file(source, dest / "agents" / source.name, args.dry_run)
        print(message)
        changes += 0 if message.startswith("sin cambios") else 1

    for skill_dir in sorted(path for path in SOURCE_SKILLS.iterdir() if path.is_dir()):
        source = skill_dir / "SKILL.md"
        if not source.is_file():
            print(f"aviso: {skill_dir} no tiene SKILL.md; se omite", file=sys.stderr)
            continue
        message = install_file(source, dest / "skills" / skill_dir.name / "SKILL.md", args.dry_run)
        print(message)
        changes += 0 if message.startswith("sin cambios") else 1

    print(f"listo: {changes} archivo(s) con cambios")
    if not args.dry_run:
        print("Reiniciá OpenCode para que descubra agentes y skills.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

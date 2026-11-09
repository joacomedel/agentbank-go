#!/usr/bin/env python3
"""Smoke end-to-end del flujo de revisión (ADR 0007, Épica 1).

Levanta un servidor con base temporal y verifica por HTTP, CLI y MCP:
transiciones, evidencia, dependencias, veredicto, reaperturas y escalado.

Uso:
    ./scripts/smoke_review.py
    AGENTBANK_BIN=/ruta/agentbank ./scripts/smoke_review.py
"""

import json
import os
import pathlib
import secrets
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:18097"
REPO = pathlib.Path(__file__).resolve().parent.parent
BIN = os.environ.get("AGENTBANK_BIN", str(REPO / "bin" / "agentbank"))
WORKDIR = pathlib.Path(tempfile.mkdtemp(prefix="agentbank-smoke-review-"))
DB = str(WORKDIR / "verify-e1.db")
TOKEN = "ab_" + secrets.token_urlsafe(32)
PASS = 0
FAIL = 0


def check(name, condition, detail=""):
    global PASS, FAIL
    if condition:
        PASS += 1
        print(f"PASS  {name}")
    else:
        FAIL += 1
        print(f"FAIL  {name}  {detail}")


def call(method, path, body=None, token=TOKEN):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, method=method, data=data)
    req.add_header("Authorization", "Bearer " + token)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        return exc.code, json.loads(raw) if raw else {}


def wait_health():
    for _ in range(60):
        try:
            urllib.request.urlopen(BASE + "/healthz", timeout=1)
            return
        except Exception:
            time.sleep(0.15)
    raise RuntimeError("el servidor no arrancó")


def valid_note(extra=""):
    return (
        "Solución: verificación e2e" + extra + "\n"
        "Cambios: ninguno\n"
        "Verificación: script de verificación e2e\n"
        "Artefactos: abc1234\n"
        "Riesgos: ninguno"
    )


def claim(ref):
    status, body = call("POST", f"/v1/tickets/{ref}/claims")
    assert status in (200, 201), (status, body)
    return body["ticket"]["version"]


def to_review(ref, sha="abc1234", note=None):
    version = claim(ref)
    status, body = call("POST", f"/v1/tickets/{ref}/avanzar", {
        "version": version, "status": "en revisión",
        "nota_cierre": note or valid_note(), "commit_sha": sha,
    })
    assert status == 200, (status, body)
    return body["ticket"]["version"]


def mcp_review(ref, verdict, comment, version):
    env = dict(os.environ)
    env.pop("AGENTBANK_AGENT", None)
    env.pop("AGENTBANK_AGENT_ROLE", None)
    env["AGENTBANK_URL"] = BASE
    env["AGENTBANK_TOKEN"] = TOKEN
    proc = subprocess.Popen(
        [BIN, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL, env=env, text=True,
    )
    messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {
            "name": "ticket_review", "arguments": {
                "id": ref, "veredicto": verdict, "comment": comment, "version": version,
            },
        }},
    ]
    for message in messages:
        proc.stdin.write(json.dumps(message) + "\n")
    proc.stdin.flush()
    deadline = time.time() + 15
    result = None
    while time.time() < deadline:
        line = proc.stdout.readline()
        if not line:
            break
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        if msg.get("id") == 2:
            result = msg
            break
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
    return result


def main():
    for suffix in ("", "-wal", "-shm"):
        path = pathlib.Path(DB + suffix)
        if path.exists():
            path.unlink()
    env = dict(os.environ)
    env["AGENTBANK_DB"] = DB
    env["AGENTBANK_ADDR"] = "127.0.0.1:18097"
    env["AGENTBANK_BOOTSTRAP_TOKEN"] = TOKEN
    server = subprocess.Popen(
        [BIN, "serve"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=env,
    )
    try:
        wait_health()
        status, project = call("POST", "/v1/projects", {"slug": "verif-e1", "name": "Verificación E1"})
        check("crear proyecto", status == 201, (status, project))
        pid = project["id"]

        status, ticket = call("POST", f"/v1/projects/{pid}/tickets", {"title": "Aprobación"})
        check("crear ticket requiere revisión", status == 201 and ticket["requiere_revision"] is True, ticket)
        ref = ticket["ref"]
        version = ticket["version"]

        status, body = call("PATCH", f"/v1/tickets/{ref}", {"version": version, "status": "hecho"})
        check("PATCH no cambia estado", status == 409 and body.get("code") == "transicion_invalida", (status, body))

        status, body = call("POST", f"/v1/tickets/{ref}/avanzar", {
            "status": "hecho", "nota_cierre": valid_note(), "commit_sha": "abc1234",
        })
        check("abierto a hecho bloqueado", status == 409 and body.get("code") == "transicion_invalida", (status, body))

        version = claim(ref)
        status, body = call("POST", f"/v1/tickets/{ref}/avanzar", {
            "version": version, "status": "en revisión",
            "nota_cierre": "Solución: incompleta", "commit_sha": "abc1234",
        })
        check("evidencia incompleta", status == 422 and body.get("code") == "evidencia_incompleta", (status, body))

        status, body = call("POST", f"/v1/tickets/{ref}/avanzar", {
            "version": version, "status": "en revisión", "nota_cierre": valid_note(),
            "commit_sha": "no-es-sha",
        })
        check("SHA inválido", status == 422 and body.get("code") == "evidencia_incompleta", (status, body))

        status, body = call("POST", f"/v1/tickets/{ref}/avanzar", {
            "version": version, "status": "en revisión", "nota_cierre": valid_note(),
            "commit_sha": "abc1234",
        })
        check("en curso a en revisión", status == 200 and body["ticket"]["status"] == "en revisión", (status, body))
        check("claim liberado al revisar", len(body["ticket"].get("claims", [])) == 0, body["ticket"].get("claims", []))
        review_version = body["ticket"]["version"]

        status, queue = call("POST", "/v1/tickets/siguiente", {
            "project_id": pid, "status": "en revisión", "claim": True,
        })
        check("cola de revisión sin claim", status == 200 and len(queue.get("candidates", [])) == 1, queue)
        status, _ = call("POST", "/v1/tickets/siguiente", {"project_id": pid, "status": "hecho"})
        check("status inválido en la cola", status == 422, status)

        cli = subprocess.run(
            [BIN, "review", "--id", ref, "--veredicto", "aprobado",
             "--comment", "CLI: verificado", "--version", str(review_version)],
            capture_output=True, text=True,
            env={**os.environ, "AGENTBANK_URL": BASE, "AGENTBANK_TOKEN": TOKEN,
                 "AGENTBANK_PROJECT": "verif-e1", "AGENTBANK_AGENT": "", "AGENTBANK_AGENT_ROLE": ""},
            timeout=20,
        )
        cli_body = json.loads(cli.stdout) if cli.returncode == 0 and cli.stdout.strip() else {}
        check("CLI review aprueba", cli.returncode == 0 and cli_body.get("ticket", {}).get("status") == "hecho",
              (cli.returncode, cli.stderr[-200:], cli_body))

        # Dependencias: padre en revisión no libera al hijo.
        status, parent = call("POST", f"/v1/projects/{pid}/tickets", {"title": "Padre"})
        status, child = call("POST", f"/v1/projects/{pid}/tickets", {
            "title": "Hijo", "depends_on": [parent["ref"]],
        })
        to_review(parent["ref"])
        status, queue = call("POST", "/v1/tickets/siguiente", {"project_id": pid})
        check("hijo bloqueado con padre en revisión",
              len(queue.get("candidates", [])) == 0 and queue.get("motivo") == "esperando_dependencias", queue)
        status, body = call("GET", f"/v1/tickets/{parent['ref']}")
        status, body = call("POST", f"/v1/tickets/{parent['ref']}/revision", {
            "veredicto": "aprobado", "comment": "ok", "version": body["version"],
        })
        check("padre aprobado", status == 200 and body["ticket"]["status"] == "hecho", (status, body))
        status, queue = call("POST", "/v1/tickets/siguiente", {"project_id": pid, "claim": False})
        refs = [c["ref"] for c in queue.get("candidates", [])]
        check("hijo liberado al hecho", child["ref"] in refs, refs)

        # Rechazos y escalado.
        status, rej = call("POST", f"/v1/projects/{pid}/tickets", {"title": "Rechazo"})
        to_review(rej["ref"])
        status, body = call("GET", f"/v1/tickets/{rej['ref']}")
        status, body = call("POST", f"/v1/tickets/{rej['ref']}/revision", {
            "veredicto": "rechazado", "comment": "falta test", "version": body["version"],
        })
        check("rechazo vuelve a abierto", status == 200 and body["ticket"]["status"] == "abierto"
              and body["ticket"]["reaperturas"] == 1, (status, body))
        to_review(rej["ref"], sha="def5678")
        status, body = call("GET", f"/v1/tickets/{rej['ref']}")
        status, body = call("POST", f"/v1/tickets/{rej['ref']}/revision", {
            "veredicto": "rechazado", "comment": "sigue faltando", "version": body["version"],
        })
        check("segundo rechazo bloquea", status == 200 and body["ticket"]["status"] == "bloqueado"
              and body["ticket"]["reaperturas"] == 2, (status, body))
        conn = sqlite3.connect(DB)
        messages = conn.execute(
            "SELECT COUNT(1) FROM messages WHERE project_id = ? AND type = 'solicitud' AND subject LIKE '%bloqueado%'",
            (pid,),
        ).fetchone()[0]
        conn.close()
        check("escalado al dueño", messages == 1, messages)

        # Ticket sin revisión.
        status, direct = call("POST", f"/v1/projects/{pid}/tickets", {
            "title": "Sin revisión", "requiere_revision": False,
        })
        version = claim(direct["ref"])
        status, body = call("POST", f"/v1/tickets/{direct['ref']}/avanzar", {
            "version": version, "status": "hecho", "nota_cierre": valid_note(),
        })
        check("requiere_revision=false cierra directo", status == 200 and body["ticket"]["status"] == "hecho", (status, body))

        # MCP ticket_review.
        status, mcp_ticket = call("POST", f"/v1/projects/{pid}/tickets", {"title": "MCP"})
        review_version = to_review(mcp_ticket["ref"])
        result = mcp_review(mcp_ticket["ref"], "aprobado", "MCP: verificado", review_version)
        text = json.dumps(result or {})
        check("MCP ticket_review aprueba", result is not None and '"status": "hecho"' in text
              or "hecho" in text, text[:300])

        status, verify = call("POST", "/v1/admin/verify-all")
        check("cadena de auditoría", status == 200 and verify.get("ok") is True, (status, verify))

        print(f"\nRESULTADO: {PASS} PASS, {FAIL} FAIL")
        print(f"run dir: {WORKDIR}")
        return 0 if FAIL == 0 else 1
    finally:
        server.terminate()
        try:
            server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server.kill()


if __name__ == "__main__":
    sys.exit(main())

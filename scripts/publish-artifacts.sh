#!/usr/bin/env bash
# Publica o verifica los artefactos declarados en un manifiesto.
#
# Uso:
#   scripts/publish-artifacts.sh <proyecto> <manifiesto>
#   scripts/publish-artifacts.sh --check <proyecto> <manifiesto>
#
# Manifiesto: una línea por artefacto con "ruta<TAB>archivo". Las líneas vacías
# y las que empiezan con # se ignoran. Ejemplo:
#   openapi.json	dist/openapi.json
#   docs/api.md	docs/api.md
#
# Variables:
#   AGENTBANK_BIN      binario a usar (default: agentbank)
#   AGENTBANK_PROJECT  proyecto por defecto si no se pasa como argumento
#   SOURCE_COMMIT      commit a registrar (default: git rev-parse HEAD)
#
# En modo --check falla (exit 1) si un artefacto falta o difiere del archivo
# local. Ese es el paso que debe correr en CI para que el registro no se
# desactualice.
set -euo pipefail

binary="${AGENTBANK_BIN:-agentbank}"
commit="${SOURCE_COMMIT:-$(git rev-parse HEAD 2>/dev/null || true)}"

check=0
if [[ "${1:-}" == "--check" ]]; then
  check=1
  shift
fi

project="${1:-${AGENTBANK_PROJECT:-}}"
manifest="${2:-}"
if [[ -z "$project" || -z "$manifest" ]]; then
  echo "uso: $0 [--check] <proyecto> <manifiesto>" >&2
  exit 2
fi
if [[ ! -f "$manifest" ]]; then
  echo "manifiesto no encontrado: $manifest" >&2
  exit 2
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
failed=0
count=0
index=0

while IFS=$'\t' read -r route file || [[ -n "${route:-}" ]]; do
  route="${route%$'\r'}"
  file="${file%$'\r'}"
  [[ -z "$route" || "$route" == \#* ]] && continue
  if [[ -z "$file" ]]; then
    echo "manifiesto inválido: falta archivo para $route" >&2
    exit 2
  fi
  if [[ ! -f "$file" ]]; then
    echo "falta el archivo local: $file (ruta $route)" >&2
    failed=1
    continue
  fi
  count=$((count + 1))
  index=$((index + 1))

  if (( check )); then
    target="$workdir/artifact-$index"
    if ! "$binary" artifact get --project "$project" --path "$route" --out "$target" >/dev/null 2>&1; then
      echo "FALTA: $route"
      failed=1
      continue
    fi
    if ! cmp -s "$file" "$target"; then
      echo "DESACTUALIZADO: $route"
      failed=1
      continue
    fi
    echo "ok: $route"
  else
    args=(artifact put --project "$project" --path "$route" --file "$file")
    if [[ -n "$commit" ]]; then
      args+=(--commit "$commit")
    fi
    if ! "$binary" "${args[@]}" >/dev/null; then
      echo "ERROR al publicar: $route" >&2
      failed=1
      continue
    fi
    echo "publicado: $route"
  fi
done < "$manifest"

if (( count == 0 )); then
  echo "el manifiesto no declara artefactos" >&2
  exit 2
fi

if (( check && failed )); then
  echo
  echo "Hay artefactos faltantes o desactualizados. Republicá con:"
  echo "  $0 $project $manifest"
  exit 1
fi
if (( failed )); then
  exit 1
fi

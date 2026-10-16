#!/usr/bin/env bash
set -euo pipefail
agent_kind=${1:?agent required}
utilities=${2:-}
lock_file=${3:-/opt/paseo-dock/install/versions.lock.json}
[[ -n "$utilities" ]] || exit 0
case "$agent_kind" in codex|claude|antigravity-cli) ;; *) echo "unsupported utility agent: $agent_kind" >&2; exit 1 ;; esac

# Validate the complete request before downloading or changing agent settings.
node - "$lock_file" "$utilities" "$agent_kind" <<'NODE'
const fs = require('fs');
const lock = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
for (const id of process.argv[3].split(',')) {
  const entry = lock.utilities?.[id];
  if (!entry || !['rtk', 'headroom'].includes(entry.installer || id)) {
    throw new Error(`unsupported utility: ${id}`);
  }
  if (!entry.version) throw new Error(`missing version for utility: ${id}`);
  if (process.argv[4] === 'antigravity-cli' && (entry.installer || id) === 'headroom') {
    throw new Error('Headroom integration for antigravity-cli is not configured');
  }
}
NODE

bin_dir="$HOME/.local/bin"
utility_dir="$HOME/.local/share/paseo-dock/utilities"
mkdir -p "$bin_dir" "$utility_dir"
export PATH="$bin_dir:$PATH"
case "$(uname -m)" in aarch64|arm64) arch=aarch64 ;; x86_64|amd64) arch=x86_64 ;; *) echo 'unsupported utility architecture' >&2; exit 1 ;; esac
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

lock_value() {
  node - "$lock_file" "$1" <<'NODE'
const fs = require('fs');
let value = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
for (const key of process.argv[3].split('.')) value = value?.[key];
if (typeof value !== 'string' || !value) throw new Error(`missing lock field: ${process.argv[3]}`);
process.stdout.write(value);
NODE
}

install_binary() {
  local binary=$1 key=$2 archive extract url checksum source
  url=$(lock_value "$key.assets.$arch.url")
  checksum=$(lock_value "$key.assets.$arch.sha256")
  archive="$temp_dir/$binary.tar.gz"
  extract="$temp_dir/$binary"
  mkdir -p "$extract"
  curl --fail --location --retry 3 --proto '=https' -o "$archive" "$url"
  printf '%s  %s\n' "$checksum" "$archive" | sha256sum --check --status -
  tar -xzf "$archive" -C "$extract"
  source=$(find "$extract" -type f -name "$binary" -print -quit)
  [[ -n "$source" ]] || { echo "missing binary: $binary" >&2; return 1; }
  install -m 0755 "$source" "$bin_dir/$binary"
}

IFS=',' read -r -a selected_utilities <<< "$utilities"
for utility in "${selected_utilities[@]}"; do
  installer=$(node -e 'const e=require(process.argv[1]).utilities[process.argv[2]];process.stdout.write(e.installer || process.argv[2]);' "$lock_file" "$utility")
  case "$installer" in
    rtk)
      install_binary rtk "utilities.$utility"
      rtk --version
      if [[ "$agent_kind" == codex ]]; then
        rtk init -g --codex
      elif [[ "$agent_kind" == antigravity-cli ]]; then
        # RTK 0.50.0 generates project-scoped rules without frontmatter.
        # Reuse its official content in Antigravity's always-active global file.
        rtk_rules_dir="$temp_dir/antigravity-rtk"
        mkdir -p "$rtk_rules_dir" "$HOME/.gemini"
        (cd "$rtk_rules_dir" && rtk init --agent antigravity)
        printf '\n' >> "$HOME/.gemini/GEMINI.md"
        cat "$rtk_rules_dir/.agents/rules/antigravity-rtk-rules.md" >> "$HOME/.gemini/GEMINI.md"
      else
        rtk init -g --auto-patch
      fi
      ;;
    headroom)
      install_binary uv "utilities.$utility.uv"
      export UV_PYTHON_INSTALL_DIR="$utility_dir/python"
      python_version=$(lock_value "utilities.$utility.python")
      headroom_version=$(lock_value "utilities.$utility.version")
      headroom_package=$(lock_value "utilities.$utility.package")
      # A bundle shares one Headroom runtime between its agents.
      if [[ ! -x "$utility_dir/headroom/venv/bin/headroom" ]]; then
        uv python install "$python_version"
        uv venv --python "$python_version" "$utility_dir/headroom/venv"
        uv pip install --python "$utility_dir/headroom/venv/bin/python" "$headroom_package==$headroom_version"
      fi
      ln -sf "$utility_dir/headroom/venv/bin/headroom" "$bin_dir/headroom"
      headroom --version
      if [[ "$agent_kind" == codex ]]; then
        codex mcp add headroom -- "$bin_dir/headroom" mcp serve
      else
        claude mcp add --transport stdio --scope user headroom -- "$bin_dir/headroom" mcp serve
      fi
      ;;
  esac
done
node - "$lock_file" "$utilities" "$agent_kind" "$utility_dir/installed.json" <<'NODE'
const fs = require('fs');
const lock = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const utilities = Object.fromEntries(process.argv[3].split(',').map(id => [id, lock.utilities[id]]));
fs.writeFileSync(process.argv[5], JSON.stringify({agent: process.argv[4], utilities}, null, 2) + '\n');
NODE

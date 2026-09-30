#!/usr/bin/env bash
set -euo pipefail
agent_kind=$1
tool_kind=$2
method_installer=${3:-}
lock_file=/opt/paseo-sbx/install/versions.lock.json
marketplace_dir="$HOME/.local/share/paseo-sbx/marketplace"
mkdir -p "$marketplace_dir/plugins" "$HOME/.codex" "$HOME/.claude"

if [[ "$method_installer" == gsd-core || ( -z "$method_installer" && "$tool_kind" == gsd-core ) ]]; then
  gsd_version=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].version)' "$lock_file" "$tool_kind")
  gsd_package=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].package)' "$lock_file" "$tool_kind")
  npx --yes "$gsd_package@$gsd_version" "--$agent_kind" --global
else
  repo_url=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].repository)' "$lock_file" "$tool_kind")
  revision=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].revision)' "$lock_file" "$tool_kind")
  git init --quiet "$marketplace_dir/plugins/$tool_kind"
  git -C "$marketplace_dir/plugins/$tool_kind" remote add origin "$repo_url"
  git -C "$marketplace_dir/plugins/$tool_kind" fetch --quiet --depth 1 origin "$revision"
  git -C "$marketplace_dir/plugins/$tool_kind" checkout --quiet --detach FETCH_HEAD
  node - "$marketplace_dir" "$tool_kind" <<'NODE'
const fs = require('fs');
const [root, name] = process.argv.slice(2);
fs.mkdirSync(`${root}/.agents/plugins`, {recursive: true});
fs.mkdirSync(`${root}/.claude-plugin`, {recursive: true});
fs.writeFileSync(`${root}/.agents/plugins/marketplace.json`, JSON.stringify({
  name: 'paseo-sbx', plugins: [{name, source: {source: 'local', path: `./plugins/${name}`}}]
}));
fs.writeFileSync(`${root}/.claude-plugin/marketplace.json`, JSON.stringify({
  name: 'paseo-sbx', owner: {name: 'paseo-sbx'}, plugins: [{name, source: `./plugins/${name}`}]
}));
NODE
  if [[ "$agent_kind" == codex ]]; then
    codex plugin marketplace add "$marketplace_dir"
    codex plugin add "$tool_kind@paseo-sbx"
  else
    claude plugin marketplace add "$marketplace_dir"
    claude plugin install "$tool_kind@paseo-sbx" --scope user
  fi
fi

if [[ "$agent_kind" == codex ]]; then
  cat /opt/paseo-sbx/install/agent-instructions.md >> "$HOME/.codex/AGENTS.md"
  if [[ "$tool_kind" == superpowers ]]; then
    printf '\nUse the using-superpowers skill before starting work.\n' >> "$HOME/.codex/AGENTS.md"
  fi
else
  cat /opt/paseo-sbx/install/agent-instructions.md >> "$HOME/.claude/CLAUDE.md"
fi
node - "$agent_kind" "$tool_kind" "$lock_file" <<'NODE'
const fs = require('fs');
const [agent, tool, lock] = process.argv.slice(2);
fs.writeFileSync(`${process.env.HOME}/.local/share/paseo-sbx/installed.json`,
  JSON.stringify({agent, tool, lock: JSON.parse(fs.readFileSync(lock, 'utf8'))}, null, 2));
NODE

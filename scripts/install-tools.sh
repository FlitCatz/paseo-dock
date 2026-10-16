    if [[ -e "$registered" ]]; then
      # Claude snapshots marketplaces; refresh it to see plugins added since.
      claude plugin marketplace update paseo-dock
    else
      claude plugin marketplace add "$marketplace_dir"; touch "$registered"
    fi
#!/usr/bin/env bash
set -euo pipefail
agent_kind=$1
tool_kind=$2
method_installer=${3:-}
lock_file=/opt/paseo-dock/install/versions.lock.json
marketplace_dir="$HOME/.local/share/paseo-dock/marketplace"
mkdir -p "$marketplace_dir/plugins" "$HOME/.codex" "$HOME/.claude"

if [[ "$method_installer" == none ]]; then
  :
elif [[ "$method_installer" == openspec ]]; then
  openspec_package=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].package)' "$lock_file" "$tool_kind")
  openspec_version=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].version)' "$lock_file" "$tool_kind")
  npm install --global --prefix "$HOME/.local" "$openspec_package@$openspec_version"
elif [[ "$method_installer" == gsd-core || ( -z "$method_installer" && "$tool_kind" == gsd-core ) ]]; then
  gsd_version=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].version)' "$lock_file" "$tool_kind")
  gsd_package=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].package)' "$lock_file" "$tool_kind")
  if [[ "$agent_kind" == antigravity-cli ]]; then
    ANTIGRAVITY_CONFIG_DIR="$HOME/.gemini/antigravity-cli" \
      npx --yes "$gsd_package@$gsd_version" --antigravity --global
    # GSD 1.15.0's generated @ references retain its default runtime root.
    ln -sfn antigravity-cli "$HOME/.gemini/antigravity"
  else
    npx --yes "$gsd_package@$gsd_version" "--$agent_kind" --global
  fi
else
  repo_url=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].repository)' "$lock_file" "$tool_kind")
  revision=$(node -e 'console.log(require(process.argv[1]).tools[process.argv[2]].revision)' "$lock_file" "$tool_kind")
  # A bundle may pair one methodology with several agents; fetch it once.
  if [[ ! -d "$marketplace_dir/plugins/$tool_kind/.git" ]]; then
    git init --quiet "$marketplace_dir/plugins/$tool_kind"
    git -C "$marketplace_dir/plugins/$tool_kind" remote add origin "$repo_url"
    git -C "$marketplace_dir/plugins/$tool_kind" fetch --quiet --depth 1 origin "$revision"
    git -C "$marketplace_dir/plugins/$tool_kind" checkout --quiet --detach FETCH_HEAD
  fi
  node - "$marketplace_dir" "$tool_kind" <<'NODE'
const fs = require('fs');
const [root] = process.argv.slice(2);
// List every fetched plugin so earlier installs in a bundle stay resolvable.
const names = fs.readdirSync(`${root}/plugins`).filter(n => fs.existsSync(`${root}/plugins/${n}/.git`)).sort();
fs.mkdirSync(`${root}/.agents/plugins`, {recursive: true});
fs.mkdirSync(`${root}/.claude-plugin`, {recursive: true});
fs.writeFileSync(`${root}/.agents/plugins/marketplace.json`, JSON.stringify({
  name: 'paseo-dock', plugins: names.map(name => ({name, source: {source: 'local', path: `./plugins/${name}`}}))
}));
fs.writeFileSync(`${root}/.claude-plugin/marketplace.json`, JSON.stringify({
  name: 'paseo-dock', owner: {name: 'paseo-dock'}, plugins: names.map(name => ({name, source: `./plugins/${name}`}))
}));
NODE
  # Register the shared marketplace once per agent; later pairs reuse it.
  registered="$marketplace_dir/.registered-$agent_kind"
  if [[ "$agent_kind" == codex ]]; then
    [[ -e "$registered" ]] || { codex plugin marketplace add "$marketplace_dir"; touch "$registered"; }
    codex plugin add "$tool_kind@paseo-dock"
  elif [[ "$agent_kind" == claude ]]; then
    [[ -e "$registered" ]] || { claude plugin marketplace add "$marketplace_dir"; touch "$registered"; }
    claude plugin install "$tool_kind@paseo-dock" --scope user
  elif [[ "$agent_kind" == antigravity-cli && "$tool_kind" == ponytail ]]; then
    agy plugin install "$marketplace_dir/plugins/$tool_kind"
  elif [[ "$agent_kind" == antigravity-cli && "$tool_kind" == superpowers ]]; then
    for dir in "$HOME/.gemini/antigravity-cli/skills"; do
      mkdir -p "$dir"
      for skill in "$marketplace_dir/plugins/$tool_kind/skills/"*; do
        [[ -f "$skill/SKILL.md" ]] || continue
        ln -s "$skill" "$dir/$(basename "$skill")"
      done
    done
  else
    echo "unsupported methodology for $agent_kind: $tool_kind" >&2; exit 1
  fi
fi

# Shared instructions are appended once per agent, even when a bundle pairs
# the agent with several methodologies.
instructions_marker="$HOME/.local/share/paseo-dock/instructions-$agent_kind"
append_instructions() {
  [[ -e "$instructions_marker" ]] && return 0
  cat /opt/paseo-dock/install/agent-instructions.md >> "$1"
  touch "$instructions_marker"
}
if [[ "$agent_kind" == codex ]]; then
  append_instructions "$HOME/.codex/AGENTS.md"
  if [[ "$tool_kind" == superpowers ]]; then
    printf '\nUse the using-superpowers skill before starting work.\n' >> "$HOME/.codex/AGENTS.md"
  fi
elif [[ "$agent_kind" == claude ]]; then
  append_instructions "$HOME/.claude/CLAUDE.md"
elif [[ "$agent_kind" == antigravity-cli ]]; then
  mkdir -p "$HOME/.gemini"
  append_instructions "$HOME/.gemini/GEMINI.md"
  if [[ "$tool_kind" == superpowers ]]; then
    printf '\nUse the using-superpowers skill before starting work.\n' >> "$HOME/.gemini/GEMINI.md"
  fi
fi
node - "$agent_kind" "$tool_kind" "$lock_file" <<'NODE'
const fs = require('fs');
const [agent, tool, lock] = process.argv.slice(2);
fs.writeFileSync(`${process.env.HOME}/.local/share/paseo-dock/installed.json`,
  JSON.stringify({agent, tool, lock: JSON.parse(fs.readFileSync(lock, 'utf8'))}, null, 2));
NODE

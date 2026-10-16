# syntax=docker/dockerfile:1
ARG BASE_IMAGE=docker.io/docker/sandbox-templates:codex
FROM --platform=$BUILDPLATFORM golang:1.26.1-bookworm AS installer
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/install-runtime ./cmd/install-runtime
COPY internal/runtimeinstall ./internal/runtimeinstall
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /install-runtime ./cmd/install-runtime

FROM ${BASE_IMAGE} AS node-runtime
USER root
COPY versions.lock.json /opt/paseo-dock/install/
COPY --from=installer /install-runtime /opt/paseo-dock/install/install-runtime
ENV PATH="/opt/paseo-dock/runtime/bin:/opt/paseo-dock/node/bin:${PATH}"
RUN mkdir -p /opt/paseo-dock/runtime /opt/paseo-dock/install \
    && apt-get update \
    && apt-get install -y --no-install-recommends xz-utils curl ca-certificates jq \
    && rm -rf /var/lib/apt/lists/* \
    && /opt/paseo-dock/install/install-runtime

FROM node-runtime
ARG AGENT
ARG AGENT_VERSION
ARG AGENT_SOURCE
ARG AGENT_MANIFEST
# A bundle lists every agent; each version comes from the copied lock.
ARG AGENTS=""
RUN set -e; \
    agents="$AGENT"; \
    if [ -n "$AGENTS" ]; then agents=$(printf '%s' "$AGENTS" | tr ',' ' '); fi; \
    for agent in $agents; do \
      version="$AGENT_VERSION"; \
      if [ -n "$AGENTS" ]; then version=$(jq -er --arg agent "$agent" '.agents[$agent]' /opt/paseo-dock/install/versions.lock.json); fi; \
      if [ "$agent" = codex ]; then \
        npm install --global --prefix /opt/paseo-dock/runtime "@openai/codex@$version"; \
      elif [ "$agent" = claude ]; then \
        npm install --global --prefix /opt/paseo-dock/runtime "@anthropic-ai/claude-code@$version"; \
      elif [ "$agent" = antigravity-cli ]; then \
        /opt/paseo-dock/install/install-runtime --agent antigravity-cli --destination /opt/paseo-dock/runtime/bin/agy; \
      elif [ "$agent" = omp ] || [ "$agent" = gjc ]; then \
        /opt/paseo-dock/install/install-runtime --agent "$agent" --destination "/opt/paseo-dock/runtime/bin/$agent" --notices "/opt/paseo-dock/licenses/$agent"; \
      else echo "Unsupported agent adapter: $agent" >&2; exit 1; fi; \
    done
# ACP servers for agents connected over acp; each must match the locked integrity.
ARG ACP_ADAPTERS=""
RUN set -e; \
    for agent in $(printf '%s' "$ACP_ADAPTERS" | tr ',' ' '); do \
      adapter=$(jq -ce --arg agent "$agent" '.acp_adapters[$agent]' /opt/paseo-dock/install/versions.lock.json); \
      package=$(printf '%s' "$adapter" | jq -er .package); \
      version=$(printf '%s' "$adapter" | jq -er .version); \
      integrity=$(printf '%s' "$adapter" | jq -er .integrity); \
      actual=$(npm view "$package@$version" dist.integrity); \
      if [ "$actual" != "$integrity" ]; then echo "ACP adapter integrity mismatch: $package@$version" >&2; exit 1; fi; \
      npm install --global --prefix /opt/paseo-dock/runtime "$package@$version"; \
    done
COPY versions.lock.json scripts/install-tools.sh scripts/install-utilities.sh scripts/agent-instructions.md /opt/paseo-dock/install/
USER agent
ENV PATH="/home/agent/.local/bin:${PATH}"
ARG TOOL
ARG METHOD_INSTALLER
ARG UTILITIES=""
ARG CATEGORY="container"
# A bundle installs each agent:methodology:installer pair into one image.
ARG PAIRS=""
ARG AGENT_MANIFESTS=""
RUN set -e; \
    if [ -z "$PAIRS" ]; then \
      bash /opt/paseo-dock/install/install-tools.sh "$AGENT" "$TOOL" "$METHOD_INSTALLER"; \
      bash /opt/paseo-dock/install/install-utilities.sh "$AGENT" "$UTILITIES"; \
      printf '%s\n' "$AGENT_MANIFEST" > /home/agent/.local/share/paseo-dock/agent.json; \
    else \
      for pair in $(printf '%s' "$PAIRS" | tr ',' ' '); do \
        agent=${pair%%:*}; rest=${pair#*:}; \
        bash /opt/paseo-dock/install/install-tools.sh "$agent" "${rest%%:*}" "${rest#*:}"; \
      done; \
      mkdir -p /home/agent/.local/share/paseo-dock/agents; \
      for agent in $(printf '%s' "$AGENTS" | tr ',' ' '); do \
        bash /opt/paseo-dock/install/install-utilities.sh "$agent" "$UTILITIES"; \
        printf '%s' "$AGENT_MANIFESTS" | jq -ce --arg agent "$agent" '.[$agent]' > "/home/agent/.local/share/paseo-dock/agents/$agent.json"; \
      done; \
      jq -n --arg pairs "$PAIRS" --slurpfile lock /opt/paseo-dock/install/versions.lock.json \
        '{pairs: ($pairs | split(",") | map(split(":") | {agent: .[0], methodology: .[1]})), lock: $lock[0]}' \
        > /home/agent/.local/share/paseo-dock/installed.json; \
    fi
LABEL org.opencontainers.image.title="Paseo development container" \
      io.paseo-dock.category="${CATEGORY}" \
      io.paseo-dock.agent="${AGENT}" io.paseo-dock.tool="${TOOL}" io.paseo-dock.utilities="${UTILITIES}" \
      io.paseo-dock.agent.version="${AGENT_VERSION}" io.paseo-dock.agent.source="${AGENT_SOURCE}"

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
COPY versions.lock.json /opt/paseo-sbx/install/
COPY --from=installer /install-runtime /opt/paseo-sbx/install/install-runtime
ENV PATH="/opt/paseo-sbx/runtime/bin:/opt/paseo-sbx/node/bin:${PATH}"
RUN mkdir -p /opt/paseo-sbx/runtime /opt/paseo-sbx/install \
    && apt-get update \
    && apt-get install -y --no-install-recommends xz-utils curl ca-certificates jq \
    && rm -rf /var/lib/apt/lists/* \
    && /opt/paseo-sbx/install/install-runtime

FROM node-runtime
ARG AGENT
ARG AGENT_VERSION
ARG AGENT_SOURCE
ARG AGENT_MANIFEST
RUN if [ "$AGENT" = codex ]; then \
      npm install --global --prefix /opt/paseo-sbx/runtime "@openai/codex@$AGENT_VERSION"; \
    elif [ "$AGENT" = claude ]; then \
      npm install --global --prefix /opt/paseo-sbx/runtime "@anthropic-ai/claude-code@$AGENT_VERSION"; \
    elif [ "$AGENT" = antigravity-cli ]; then \
      /opt/paseo-sbx/install/install-runtime --agent antigravity-cli --destination /opt/paseo-sbx/runtime/bin/agy; \
    else echo "Unsupported agent adapter: $AGENT" >&2; exit 1; fi
COPY versions.lock.json scripts/install-tools.sh scripts/install-utilities.sh scripts/agent-instructions.md /opt/paseo-sbx/install/
USER agent
ENV PATH="/home/agent/.local/bin:${PATH}"
ARG TOOL
ARG METHOD_INSTALLER
ARG UTILITIES=""
ARG CATEGORY="container"
RUN bash /opt/paseo-sbx/install/install-tools.sh "$AGENT" "$TOOL" "$METHOD_INSTALLER" \
    && bash /opt/paseo-sbx/install/install-utilities.sh "$AGENT" "$UTILITIES" \
    && printf '%s\n' "$AGENT_MANIFEST" > /home/agent/.local/share/paseo-sbx/agent.json
LABEL org.opencontainers.image.title="Paseo development container" \
      io.paseo-sbx.category="${CATEGORY}" \
      io.paseo-sbx.agent="${AGENT}" io.paseo-sbx.tool="${TOOL}" io.paseo-sbx.utilities="${UTILITIES}" \
      io.paseo-sbx.agent.version="${AGENT_VERSION}" io.paseo-sbx.agent.source="${AGENT_SOURCE}"

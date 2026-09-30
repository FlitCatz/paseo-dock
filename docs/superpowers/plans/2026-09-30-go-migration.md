# Go Migration Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement project commands and runtime in Go while preserving sandbox integration.
**Architecture:** Three command binaries backed by config, cli, launcher, gateway and nodeinstall packages. Standard library only; tar handles checksum-verified xz archives.
**Tech Stack:** Go 1.26+, HTTP, JSON, os/exec, POSIX process groups.
**Spec:** docs/superpowers/specs/2026-09-30-go-migration-design.md

## Global Constraints

Preserve six profiles, registry structure, workspace hash and CLI arguments. Keep launcher outside writable workspaces. Preserve upstream credential isolation and delegation allowlist. Do not apply host configuration or publish images during migration.

## Review Focus

Malformed JSON and missing arguments must fail closed. Symlink and nonexistent paths must respect workspace boundaries. Child exit and blocked stdin must not hang shutdown. Concurrent relay binding must create one temporary rule. Installation must work from outside the source directory.

## Task 1 Config and CLI

Files: internal/config/config.go, internal/cli/cli.go, internal/cli/cli_test.go, cmd/paseo-sbx/main.go, Makefile.
Interfaces: config.Load(root), config.RuntimePath(workspace), config.WriteJSON(path,value); cli.Run(root,args,stdin,stdout,stderr).
- [x] Write tests for registry naming, bake generation, invalid arguments, configuration merging and fake executable command routing.
- [x] Run go test ./internal/cli and observe missing implementation.
- [x] Implement lock loading, canonical paths, atomic JSON writes and all seven CLI commands.
- [x] Run package tests and verify command help and config outputs.

## Task 2 Policy and HTTP relay

Files: internal/gateway/policy.go, relay.go, policy_test.go, relay_test.go.
Interfaces: NewPolicy(providers,caller), Policy.Check, Policy.Filter, RewriteResponse, NewRelay(providers,sandbox,runner), Relay.Bind, Relay.Close.
- [x] Port existing boundary cases and add HTTP authentication, malformed inputs, credential redaction and cleanup tests.
- [x] Run package tests and observe missing implementation.
- [x] Implement request allowlist, filtered JSON/SSE responses and loopback relay with one policy rule per session.
- [x] Run package tests with race detector.

## Task 3 Launcher

Files: internal/launcher/launcher.go, launcher_test.go, cmd/agent-wrapper/main.go.
Interfaces: RewriteTree(value,bind), CheckWorkdirs(value,workspace), Launch(runtime,profile,args).
- [x] Write protocol rewrite, workspace escape, fake sbx stdin and process-exit tests.
- [x] Observe failing tests then implement Claude configuration translation, Codex JSON-line forwarding, process groups and relay lifecycle.
- [x] Verify package tests including child exit while stdin remains open.

## Task 4 Installer and delivery

Files: internal/nodeinstall/install.go, install_test.go, cmd/install-node/main.go, Dockerfile, .dockerignore, .github/workflows/templates.yml, README.md.
Interfaces: nodeinstall.Install(lockPath,destination,machine).
- [x] Write checksum, architecture, installation and failed-download tests using local fixtures.
- [x] Observe failures then implement verified download and tar extraction.
- [x] Add Go multi-stage Docker build and Go CI checks; remove superseded source and entrypoints.
- [x] Run go test ./..., go test -race ./..., go vet ./... and four-platform builds; inspect Docker build configuration.

## Execution record

User authorized conversion after spec review. Execute inline. No Git repository exists, so commit and worktree steps do not apply. Final independent review is limited to defects against the approved spec.

## Verification record

All four tasks complete in the existing non-Git workspace. RED runs established missing Go implementations; regressions subsequently established symlink-parent traversal, malformed and text-only responses, duplicate MCP headers, short/escaped secrets and registry truncation before fixes. GREEN: go test ./..., go test -race ./..., go vet ./..., make build, all three commands for darwin/linux arm64/amd64. Actual Docker installer and Codex linux/arm64 node-runtime stages passed, including official Node download and checksum verification.

Ruling: Preserve original optional cwd:null semantics while rejecting other non-path values. Resolve symlinks component-by-component before parent traversal. Prevalidate reuse and retain existing registry entries on partial creation. Reject tool successes without object structured results. Node runtime requires xz-utils because both original base images lack the xz executable. No Git commits or worktree creation apply to this workspace.

Independent review identified and fixed the above boundary defects. No publication, installed host config change or live model invocation was performed.

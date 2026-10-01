package config

func SupportedAdapter(adapter string) bool {
	return adapter == "codex" || adapter == "claude" || adapter == "antigravity-cli"
}

// Antigravity runs its native CLI in a shell sandbox.
func SandboxAgent(adapter string) string {
	if adapter == "antigravity-cli" {
		return "shell"
	}
	return adapter
}

func ProviderAdapter(adapter string) string {
	return adapter
}

func AgentCommand(adapter string) string {
	if adapter == "antigravity-cli" {
		return "agy"
	}
	return adapter
}

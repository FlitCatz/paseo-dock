# Sandbox execution and delegation

You run inside a Docker Sandbox. Use your built-in file and terminal tools inside
this sandbox. Stay within the connected project. User instructions take priority.

For independent work that benefits from delegation, use the Paseo MCP tools:
1. Call list_providers and list_models to discover available sandbox profiles and models.
2. Call create_agent with provider in the form PROFILE/MODEL and an initialPrompt
   describing the task, expected output, relevant files, and constraints. Use only
   profiles returned by list_providers. Do not set cwd or workspaceId.
3. Continue independent work while the worker runs. Use get_agent_status or
   get_agent_activity when you need its result. Respect finish notifications.
4. Review the worker's changes/results before integrating them. Give write tasks
   disjoint files; workers in this workspace share the project directory.

The host relay permits sandbox-profile delegation and status only. It does not
provide host terminals, scripts, browsers, scheduling, or alternate workspaces.
Credentials for the host Paseo daemon are retained by the relay, not this agent.

Help the user set up and use the Errly MCP server so AI agents can query and manage errors.

## Your goal
Get the Errly MCP server connected to the user's AI client (Claude Code, Claude Desktop, or Cursor) and demonstrate how to use it.

## Prerequisites
- Errly server must be running at `http://localhost:5080` (run `/install-errly` first)
- Docker must be installed

## Step 1 — Build the MCP image
```bash
docker build -f Dockerfile.mcp -t errly-mcp .
```

Verify it built:
```bash
docker images | grep errly-mcp
```

## Step 2 — Configure the MCP client

### Claude Code (project-level — already done if using this repo)
The `.mcp.json` in this repo is pre-configured. Just ensure the API key matches:
```json
{
  "mcpServers": {
    "errly": {
      "command": "docker",
      "args": [
        "run", "-i", "--network", "host",
        "-e", "ERRLY_URL=http://localhost:5080",
        "-e", "ERRLY_API_KEY=your-secret-key",
        "errly-mcp"
      ]
    }
  }
}
```

### Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`)
```json
{
  "mcpServers": {
    "errly": {
      "command": "docker",
      "args": [
        "run", "-i", "--network", "host",
        "-e", "ERRLY_URL=http://localhost:5080",
        "-e", "ERRLY_API_KEY=your-secret-key",
        "errly-mcp"
      ]
    }
  }
}
```

### Remote Errly server
Replace `--network host` and `ERRLY_URL`:
```json
"-e", "ERRLY_URL=https://errly.yourcompany.com"
```
Remove `--network host` since it is not needed for external URLs.

## Step 3 — Restart the AI client
After editing the config, restart Claude Code or Claude Desktop to load the MCP server.

## Step 4 — Verify the connection
Ask in Claude: _"List my Errly issues"_ or _"What tools does the errly MCP provide?"_

## Available MCP tools

| Tool | What it does | Example prompt |
|------|-------------|----------------|
| `list_issues` | List issues filtered by status, project, env | _"Show all unresolved errors in production"_ |
| `get_issue` | Full issue details + last event | _"Get details for issue abc123"_ |
| `get_issue_events` | Recent occurrences with stack traces | _"Show the last 5 events for issue abc123"_ |
| `search_issues` | Search by keyword | _"Find errors related to payment"_ |
| `resolve_issue` | Resolve, ignore, or reopen | _"Resolve issue abc123"_ |
| `get_stats` | Total / unresolved / events in 24h | _"How many errors happened today?"_ |

## Example workflows

**Triage session:**
> "Show me all unresolved errors. For the most frequent one, get its stack trace and suggest a fix."

**Cleanup:**
> "Find all issues that have been resolved for more than 7 days and list them."

**On-call:**
> "How many new errors occurred in the last 24 hours? Show me the top 3 by frequency."

**Debug a specific error:**
> "Search for errors related to 'database timeout', get the stack trace from the latest event, and explain what's causing it."

## Troubleshooting
- **Tools not appearing** → rebuild the image and restart the AI client
- **Unauthorized** → confirm `ERRLY_API_KEY` in the MCP config matches the server key
- **Connection refused** → ensure Errly server is running (`curl http://localhost:5080/healthz`)
- **Network issues in Docker** → `--network host` only works on Linux; on macOS use `host.docker.internal`:
  ```
  "-e", "ERRLY_URL=http://host.docker.internal:5080"
  ```
  and remove `--network host`

# Skill: Use MCP

## Trigger
User wants to connect an AI agent (Claude Code, Claude Desktop, Cursor) to their Errly server via MCP.

## Agent Behavior

### 1. Verify Errly server is running
```bash
curl http://localhost:5080/healthz
# → ok
```
If not running → redirect to `install-errly` skill first.

### 2. Build the MCP Docker image
```bash
docker build -f Dockerfile.mcp -t errly-mcp .
```

Verify:
```bash
docker images | grep errly-mcp
```

### 3. Configure the AI client

**Claude Code — project-level (`.mcp.json` already in this repo)**
Edit ERRLY_API_KEY to match the server:
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

**Claude Desktop** — add to `~/Library/Application Support/Claude/claude_desktop_config.json`:
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

**Remote Errly server** — remove `--network host`, set real URL:
```json
"-e", "ERRLY_URL=https://errly.yourcompany.com"
```

**macOS — Docker cannot use `--network host`**
Replace with:
```json
"--add-host", "host.docker.internal:host-gateway",
"-e", "ERRLY_URL=http://host.docker.internal:5080"
```

### 4. Restart the AI client
Restart Claude Code or Claude Desktop after editing the config.

### 5. Verify connection
Ask: _"List my Errly issues"_ or _"What Errly tools are available?"_

---

## Available Tools

| Tool | Description |
|------|-------------|
| `list_issues` | List issues — filter by `status`, `project`, `env`, `limit` |
| `get_issue` | Full issue details + last event data |
| `get_issue_events` | Recent occurrences with stack traces |
| `search_issues` | Full-text search by keyword |
| `resolve_issue` | Set status: `resolved` / `ignored` / `unresolved` |
| `get_stats` | Total issues, unresolved count, events in last 24h |

---

## Example Prompts

**Triage**
> "Show all unresolved errors in the payment project. For the top 3 by count, get their stack traces and suggest fixes."

**Daily standup**
> "How many new errors occurred in the last 24 hours? Which project has the most?"

**Cleanup**
> "List all ignored issues and resolve any that haven't had a new event in the last 7 days."

**Debug**
> "Search for errors containing 'timeout'. Get the last 5 events for the most frequent one and explain the root cause."

**On-call**
> "Are there any fatal or panic-level errors? If so, get their details and open a summary."

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Tools not appearing | Rebuild image → restart client |
| `unauthorized` errors | Check `ERRLY_API_KEY` matches server |
| `connection refused` | Ensure Errly server is running on port 5080 |
| macOS network issue | Use `host.docker.internal` instead of `localhost` |
| MCP not starting | Run `docker run -i errly-mcp` manually to see errors |

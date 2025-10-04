# AI Sessions MCP Server

A Model Context Protocol (MCP) server that provides access to your AI assistant CLI sessions from Claude Code, Gemini CLI, OpenAI Codex, and opencode.

*Mostly written using Claude Code.*

## What It Does

Allow AI tools to search, list, and read your previous local coding sessions from multiple CLI tools. Useful for:

- Finding past solutions to similar problems
- Reviewing what you worked on recently
- Learning from previous conversations
- Resuming interrupted work

## Demo

<p align="center">
  <img src="https://github.com/user-attachments/assets/946e09af-cf60-4c38-8a22-d5f90be05c23" alt="AI Sessions MCP demo"><br>
  <em>Resuming a Claude Code session in Codex CLI.</em>
</p>

![ai-sessions-mcp-demo](https://github.com/user-attachments/assets/946e09af-cf60-4c38-8a22-d5f90be05c23)


## Installation

### Download Pre-built Binary (No Go Required)

Download the latest release for your platform from [GitHub Releases](https://github.com/yoavf/ai-sessions-mcp/releases).


Unzip and move the binary somewhere in your path and point your MCP config at that location.

### Build from Source

**Prerequisites**: Go 1.25 or later

```bash
cd ai-sessions-mcp
go build -o bin/ai-sessions-mcp ./cmd/ai-sessions-mcp
```

Move the resulting binary anywhere you like and use that path in your MCP config.

### Setup

#### Claude Code

```bash
claude mcp add ai-sessions /path/to/ai-sessions-mcp
```

#### Claude Desktop

Add to your Claude Desktop config file:

- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Linux**: `~/.config/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "ai-sessions": {
      "command": "/path/to/ai-sessions-mcp"
    }
  }
}
```

**Restart Claude Desktop** to activate.

## Usage

Once configured, you can ask:

- "Let's continue my latest sesion from Claude Code"
- "Show me my recent Codex sessions"
- "Search my sessions for authentication bugs"
- "How many times did Claude tell me I was [absolutely right](https://absolutelyright.lol) yesterday?"

## How It Works

The server reads session files stored locally by various CLI tools:

- **Claude Code**: `~/.claude/projects/[PROJECT_DIR]/*.jsonl`
- **Gemini CLI**: `~/.gemini/tmp/[PROJECT_HASH]/chats/session-*.json`
- **OpenAI Codex**: `~/.codex/sessions/` and `~/.codex/archived_sessions/`
- **opencode**: `~/.local/share/opencode/storage/`

When you ask your ai tool to list or search sessions, it automatically uses these tools to access your session history.

## Available Tools

### `list_available_tools`
Shows which AI CLI tools have sessions on your system.

### `list_sessions`
Lists recent sessions from all projects (newest first).

**Arguments**:
- `source` (optional): Filter by `claude`, `gemini`, `codex`, or `opencode`
- `project_path` (optional): Filter by specific project directory
- `limit` (optional): Max results (default: 10)

**Example**: `{"source": "claude", "limit": 20}`

### `search_sessions`
Searches session content using BM25 ranking. Returns results sorted by relevance score with contextual snippets.

**Arguments**:
- `query` (required): Search term (supports multiple keywords)
- `source` (optional): Filter by source
- `project_path` (optional): Filter by project
- `limit` (optional): Max results (default: 10)

**Example**: `{"query": "authentication bug"}`

**Returns**: Each match includes:
- `session`: Session metadata (ID, source, project, timestamp)
- `score`: Relevance score (higher = more relevant)
- `snippet`: Contextual excerpt (~300 chars) showing where the match occurred

### `get_session`
Retrieves full session content with pagination.

**Arguments**:
- `session_id` (required): Session ID from list results
- `source` (required): Which source created it
- `page` (optional): Page number (default: 0)
- `page_size` (optional): Messages per page (default: 20)

## Troubleshooting

**Server disconnects immediately**
- Check that the binary path in the config is correct and absolute
- Ensure the binary is executable: `chmod +x bin/ai-sessions-mcp`
- Verify the JSON config is valid (no trailing commas)

**No sessions found**
- Confirm sessions exist: `ls ~/.claude/projects/` or `ls ~/.gemini/tmp/`
- By default, lists sessions from **all projects**
- To filter by project, add `"project_path": "/path/to/project"`

**Sessions are slow**
- Use smaller `limit` values
- Use `page_size` parameter for large sessions
- Filter by specific `source` or `project_path`

## License

MIT

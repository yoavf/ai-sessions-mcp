# AI Sessions MCP Server

A Model Context Protocol (MCP) server that provides access to your AI assistant CLI sessions from Claude Code, Gemini CLI, OpenAI Codex, and Cursor CLI.

*Mostly written by Claude Code.*

## What It Does

Allows AI assistants (like Claude Desktop) to search, list, and read your previous coding sessions from multiple CLI tools. Useful for:

- Finding past solutions to similar problems
- Reviewing what you worked on recently
- Learning from previous conversations
- Resuming interrupted work

## How It Works

The server reads session files stored locally by various CLI tools:

- **Claude Code**: `~/.claude/projects/[PROJECT_DIR]/*.jsonl`
- **Gemini CLI**: `~/.gemini/tmp/[PROJECT_HASH]/chats/session-*.json`
- **OpenAI Codex**: `~/.codex/sessions/` and `~/.codex/archived_sessions/`
- **Cursor CLI**: Not yet implemented (placeholder included)

When you ask Claude to list or search sessions, it automatically uses these tools to access your session history.

## Available Tools

### `list_available_tools`
Shows which AI CLI tools have sessions on your system.

### `list_sessions`
Lists recent sessions from all projects (newest first).

**Arguments**:
- `tool` (optional): Filter by `claude`, `gemini`, `codex`, or `cursor`
- `project_path` (optional): Filter by specific project directory
- `limit` (optional): Max results (default: 10)

**Example**: `{"tool": "claude", "limit": 20}`

### `search_sessions`
Searches session content for specific queries.

**Arguments**:
- `query` (required): Search term
- `tool` (optional): Filter by tool
- `project_path` (optional): Filter by project
- `limit` (optional): Max results (default: 10)

**Example**: `{"query": "authentication bug"}`

### `get_session`
Retrieves full session content with pagination.

**Arguments**:
- `session_id` (required): Session ID from list results
- `tool` (required): Which tool created it
- `page` (optional): Page number (default: 0)
- `page_size` (optional): Messages per page (default: 50)

## Installation

### Prerequisites

- Go 1.25 or later
- One or more AI CLI tools installed with existing sessions

### Build

```bash
cd ai-sessions-mcp
go build -o bin/ai-sessions-mcp ./cmd/server
```

### Configure Claude Desktop

Add to your Claude Desktop config file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
**Linux**: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "ai-sessions": {
      "command": "/full/path/to/ai-sessions-mcp/bin/ai-sessions-mcp"
    }
  }
}
```

Replace `/full/path/to/` with your actual path (use `pwd` in the project directory).

**Restart Claude Desktop** to activate.

## Usage

Once configured, simply ask Claude in natural language:

- "What AI CLI tools do I have sessions for?"
- "Show me my recent Claude Code sessions"
- "Search my sessions for authentication bugs"
- "What was I working on yesterday?"

Claude will automatically use the appropriate tools to answer.

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
- Filter by specific `tool` or `project_path`

## License

MIT

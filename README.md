# AI Sessions MCP Server

A Model Context Protocol (MCP) server that provides access to your AI assistant CLI sessions from Claude Code, Gemini CLI, OpenAI Codex, and opencode.

*Mostly written using Claude Code.*

## What It Does

Allow AI tools to search, list, and read your previous local coding sessions from multiple CLI tools. Useful for:

- Finding past solutions to similar problems
- Reviewing what you worked on recently
- Learning from previous conversations
- Resuming interrupted work

## Installation

### Download Pre-built Binary (No Go Required)

Download the latest release for your platform from [GitHub Releases](https://github.com/yoavf/ai-sessions-mcp/releases).


### Install with `go install`

```bash
go install github.com/yoavf/ai-sessions-mcp/cmd/ai-sessions-mcp@latest
```

Then use `~/go/bin/ai-sessions-mcp` as the command in your Claude Desktop config (or add `~/go/bin` to your PATH).

### Build from Source

**Prerequisites**: Go 1.25 or later

```bash
cd ai-sessions-mcp
go build -o bin/ai-sessions-mcp ./cmd/ai-sessions-mcp
```

The resulting binary in `~/go/bin/ai-sessions-mcp` is self-contained and can be copied anywhere.

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

- "What AI CLI tools do I have sessions for?"
- "Show me my recent Claude Code sessions"
- "Search my sessions for authentication bugs"
- "How many times did Claude tell me I was [absolutely right](https://absolutelyright.lol) yesterday?"

## How It Works

The server reads session files stored locally by various CLI tools:

- **Claude Code**: `~/.claude/projects/[PROJECT_DIR]/*.jsonl`
- **Gemini CLI**: `~/.gemini/tmp/[PROJECT_HASH]/chats/session-*.json`
- **OpenAI Codex**: `~/.codex/sessions/` and `~/.codex/archived_sessions/`
- **opencode**: `~/.local/share/opencode/storage/`

When you ask Claude to list or search sessions, it automatically uses these tools to access your session history.

## Available Tools

### `list_available_tools`
Shows which AI CLI tools have sessions on your system.

### `list_sessions`
Lists recent sessions from all projects (newest first).

**Arguments**:
- `tool` (optional): Filter by `claude`, `gemini`, `codex`, or `opencode`
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

# AI Sessions MCP Server

An MCP server that makes sessions from Claude Code, OpenAI Codex, Gemini CLI and opencode available to any MCP compatible client.

*Mostly written using Claude Code.*

## What It Does

Allow AI agents to search, list, and read your previous local coding sessions from multiple CLI coding agents. Useful for:

- Finding past solutions to similar problems
- Reviewing what you worked on recently
- Learning from previous conversations
- Resuming interrupted work

## Demo

<p align="center">
  <img src="https://github.com/user-attachments/assets/c75edc64-32f0-4deb-93d6-301c1e01ea81" width=800 alt="AI Sessions MCP demo"><br>
  <em>Resuming a Claude Code session in Codex CLI.</em>
</p>

## Installation

### Quick Install

**macOS, Linux, and Windows (Git Bash/WSL):**

```bash
curl -fsSL https://aisessions.dev/install.sh | bash
```

This installs the binary to `~/.aisessions/bin`. Follow the instructions to add it to your PATH.

**Custom installation directory:**

```bash
INSTALL_DIR=/custom/path curl -fsSL https://aisessions.dev/install.sh | bash
```

### Manual Download

Download pre-built binaries from [GitHub Releases](https://github.com/yoavf/ai-sessions-mcp/releases).

### Build from Source

**Prerequisites**: Go 1.25 or later

```bash
go build -o bin/aisessions ./cmd/ai-sessions
```

### Setup

After installation, configure your MCP client to use the binary:

#### Claude Code

```bash
claude mcp add ai-sessions ~/.aisessions/bin/aisessions
```

Or if using a custom install location:
```bash
claude mcp add ai-sessions /path/to/aisessions
```

#### Codex CLI

Edit `~/.codex/config.toml`:
```toml
[mcp_servers.ai_sessions]
command = "~/.aisessions/bin/aisessions"
```

#### Claude Desktop

Add to your config file (`Settings` → `Developer` → `Edit Config`):
```json
{
  "mcpServers": {
    "ai-sessions": {
      "command": "/Users/YOUR_USERNAME/.aisessions/bin/aisessions"
    }
  }
}
```

Replace `YOUR_USERNAME` with your actual username, or use your custom install path.

**Restart Claude Desktop** to activate.

## CLI Upload

The `ai-sessions` binary includes a CLI tool for uploading Claude Code transcripts to [aisessions.dev](https://aisessions.dev) for sharing.

### Authentication

```bash
aisessions login
```

Opens your browser to generate a CLI token. The token is saved locally in `~/.aisessions/config.json`.

### Uploading Sessions

**Interactive mode** (no file argument):

```bash
aisessions upload
```

Displays a searchable list of your recent Claude Code sessions. Use arrow keys to navigate and select a session to upload.

**Direct mode** (with file path):

```bash
aisessions upload /path/to/session.jsonl
aisessions upload /path/to/session.jsonl --title "Custom Title"
```

### Options

- `--title <title>` - Set a custom title for the uploaded transcript

## MCP Usage

Once configured as an MCP server, you can ask:

- "Let's continue my latest sesion from Claude Code"
- "Show me my recent Codex sessions"
- "Search my sessions for authentication bugs"
- "How many times did Claude tell me I was [absolutely right](https://absolutelyright.lol) yesterday?"

## How It Works

The server reads session files stored locally by various CLI coding agents:

- **Claude Code**: `~/.claude/projects/[PROJECT_DIR]/*.jsonl`
- **Gemini CLI**: `~/.gemini/tmp/[PROJECT_HASH]/chats/session-*.json`
- **OpenAI Codex**: `~/.codex/sessions/` and `~/.codex/archived_sessions/`
- **opencode**: `~/.local/share/opencode/storage/`

When you ask your AI agent to list or search sessions, it automatically uses these agents to access your session history.

## Available Tools

### `list_available_sources`
Shows which AI CLI coding agents have sessions on your system.

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
- `source` (required): Which coding agent created it
- `page` (optional): Page number (default: 0)
- `page_size` (optional): Messages per page (default: 20)

## Development

To keep formatting consistent and catch regressions early:

- Install [pre-commit](https://pre-commit.com/) and run `pre-commit install` to enable hooks (`gofmt`, `go vet`, `go test`).
- All pushes and pull requests run the GitHub Actions workflow (`.github/workflows/build.yml`), which checks formatting, runs `go vet`, builds the binary, and executes `go test -cover ./...`.

## License

MIT

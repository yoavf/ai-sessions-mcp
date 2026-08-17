# AI Sessions MCP Server

An MCP server that makes sessions from Claude Code, OpenAI Codex, Gemini CLI, opencode, Mistral Vibe, and GitHub Copilot CLI available to any MCP-compatible client.

*Mostly written using Claude Code.*

## What It Does

Allows AI agents to search, list, and read your previous local coding sessions from multiple CLI coding agents. Useful for:

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

**macOS (Intel and Apple Silicon), Linux (amd64 and arm64, including WSL), and Windows amd64 (Git Bash):**

```bash
curl -fsSL https://aisessions.dev/install.sh | bash
```

This installs the binary to `~/.aisessions/bin`. Follow the instructions to add it to your PATH.

**Custom installation directory:**

```bash
curl -fsSL https://aisessions.dev/install.sh | INSTALL_DIR=/custom/path bash
```

### Manual Download

Download pre-built binaries from [GitHub Releases](https://github.com/yoavf/ai-sessions-mcp/releases).

### Build from Source

**Prerequisites**: Go 1.25.13 or later

```bash
go build -o bin/aisessions ./cmd/ai-sessions
```

### Setup

After installation, configure your MCP client to use the binary:

#### Claude Code CLI

```bash
claude mcp add --scope user --transport stdio ai-sessions -- ~/.aisessions/bin/aisessions
```

Or if using a custom install location:

```bash
claude mcp add --scope user --transport stdio ai-sessions -- /path/to/aisessions
```

Verify the connection with `claude mcp get ai-sessions`.

#### Codex CLI and ChatGPT desktop app

Add the server from the CLI:

```bash
codex mcp add ai-sessions -- ~/.aisessions/bin/aisessions
```

Codex CLI and the ChatGPT desktop app share `~/.codex/config.toml`, so the server is available in both after you restart the desktop app. In ChatGPT, open **Settings** → **MCP servers** to check its status, or type `/mcp` in the composer.

For manual configuration, use an absolute path (the command is launched directly, without shell expansion):

```toml
[mcp_servers.ai_sessions]
command = "/Users/YOUR_USERNAME/.aisessions/bin/aisessions"
```

Replace `YOUR_USERNAME` with your actual username, or use your custom install path.

#### Claude Desktop

Add to the config file opened from **Settings** → **Developer** → **Edit Config**:

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

Restart Claude Desktop, then open **Developer** settings or **+** → **Connectors** in a conversation to check the connection. Claude Desktop also supports packaged `.mcpb` extensions, but direct configuration remains useful for a standalone downloaded binary.

## CLI Upload

The `aisessions` binary includes a CLI tool for uploading supported local agent transcripts to [aisessions.dev](https://aisessions.dev) for sharing.

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

Displays a searchable list of recent upload-compatible sessions from Claude Code, Codex, Gemini CLI, Mistral Vibe, and GitHub Copilot CLI. Use arrow keys to navigate and select a session to upload. opencode sessions remain available through the MCP server but are omitted from the upload picker because its current store is a shared SQLite database rather than one transcript file per session.

**Direct mode** (with file path):

```bash
aisessions upload /path/to/session.jsonl
aisessions upload /path/to/session.jsonl --title "Custom Title"
```

### Options

- `--title <title>` - Set a custom title for the uploaded transcript
- `--url <url>` - Override the API URL (`https://aisessions.dev` or a local development server)

## MCP Usage

Once configured as an MCP server, you can ask:

- "Let's continue my latest session from Claude Code"
- "Show me my recent Codex sessions"
- "Search my sessions for authentication bugs"
- "How many times did Claude tell me I was [absolutely right](https://absolutelyright.lol) yesterday?"

## How It Works

The server reads session files stored locally by various CLI coding agents:

- **Claude Code**: `~/.claude/projects/[PROJECT_DIR]/*.jsonl`
- **Gemini CLI**: `~/.gemini/tmp/[PROJECT_HASH]/chats/session-*.jsonl` (plus legacy `.json` recordings)
- **OpenAI Codex**: `~/.codex/sessions/` and `~/.codex/archived_sessions/`
- **opencode**: `~/.local/share/opencode/opencode.db` (plus the legacy `storage/` JSON tree)
- **Mistral Vibe**: `~/.vibe/logs/session/`
- **GitHub Copilot CLI**: `~/.copilot/session-state/[SESSION_ID]/events.jsonl` (plus legacy flat JSONL files)

When you ask your AI agent to list or search sessions, the server reads these local stores through source-specific adapters; it does not launch the agent CLIs.

## Available Tools

### `list_available_sources`

Shows which AI CLI source adapters are available from the running server.

### `list_sessions`

Lists recent sessions from all projects (newest first).

**Arguments**:

- `source` (optional): Filter by `claude`, `gemini`, `codex`, `opencode`, `mistral`, or `copilot`
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
- Pushes to `main` and pull requests targeting `main` run the GitHub Actions workflow (`.github/workflows/build.yml`), which checks formatting, runs `go vet`, builds the binary, and executes `go test -cover ./...`.

## License

MIT

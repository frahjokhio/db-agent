# Database AI Agent

Safe database tools for the AI you already use (Claude, ChatGPT, Cursor, Claude Code, Codex).
Ask questions about your app's MySQL database in plain English. One executable file, no runtime needed.

## Quick start

Run in your project folder (needs Node only to install; the agent itself is a native binary):

```bash
npx -y @db-agent/cli init          # enter DB details once (saved in .db-agent/config.env)
npx -y @db-agent/cli mcp-config    # paste the output into your AI tool
npx -y @db-agent/cli rules init    # optional: business definitions for better answers
```

Then ask your AI: *"Who are my top 5 customers by revenue this month?"*

### Install without Node

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/frahjokhio/db-agent/main/install.sh | sh
# Windows (PowerShell)
irm https://raw.githubusercontent.com/frahjokhio/db-agent/main/install.ps1 | iex
# Go developers
go install github.com/frahjokhio/db-agent/cmd/db-agent@latest
```
Then: `db-agent init` and `db-agent mcp-config`.

- Works with any app that uses MySQL (Laravel, Node, Python, ...). Your app's own `.env` is never touched.
- Read-only by default: every query is checked (single SELECT, no system tables, no secret columns, row and time limits) and logged.
- Use a MySQL user with SELECT-only rights for extra safety.
- Add `.db-agent/config.env` and `.db-agent/*.db` to `.gitignore`. `rules.yaml` is safe to commit.

## Commands

| Command | Purpose |
|---|---|
| `db-agent` | Status + next steps |
| `db-agent init` | Save DB connection to `.db-agent/config.env` and test it |
| `db-agent check` | Test the connection |
| `db-agent schema [table]` | Show structure |
| `db-agent rules` | Show business rules and check them against the database |
| `db-agent rules init` | Create a starter `.db-agent/rules.yaml` |
| `db-agent query "SELECT ..."` | Run SQL with the same safety checks the AI uses |
| `db-agent history` | Recent queries (ok / blocked / failed) |
| `db-agent mcp` | MCP server (your AI tool starts this) |
| `db-agent mcp-config` | Print setup for Claude Code / Cursor / Claude Desktop / Codex |

Global flag: `--dir <project folder>` (default: current folder).

## Development

Needs Go 1.27+ and Docker (for the sample MySQL only).

```bash
docker compose up -d                          # sample MySQL on port 3307
mkdir .db-agent && copy config.env.example .db-agent\config.env   # macOS/Linux: cp
go run ./cmd/db-agent check
go test ./...
go build -ldflags "-s -w" -o bin/db-agent.exe ./cmd/db-agent
```

Cross-build: `GOOS=linux GOARCH=amd64 go build -o bin/db-agent-linux ./cmd/db-agent`

## npm packaging

`@db-agent/cli` is a tiny Node launcher. The Go binary ships in one package per OS/CPU
(`@db-agent/win32-x64`, `@db-agent/linux-x64`, ...); npm installs only the matching one.

```bash
node scripts/build-npm.mjs 0.1.0        # builds all 6 binaries + packages into dist/npm/
node scripts/mcp-smoke.mjs <cmd...>     # quick MCP check of any start command
```

Publish (manual, in this order):
```bash
npm login
# 1) every platform package
cd dist/npm/win32-x64 && npm publish     # repeat for win32-arm64, darwin-x64, darwin-arm64, linux-x64, linux-arm64
# 2) then the launcher
cd dist/npm/cli && npm publish
```

See `database_ai_agent_technical_blueprint.md` (plan) and `PROJECT_STATUS.md` (progress).

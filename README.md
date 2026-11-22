# db-agent

[![npm](https://img.shields.io/npm/v/@db-agent/cli)](https://www.npmjs.com/package/@db-agent/cli)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**Ask your MySQL database questions in plain English, using the AI you already have:** Claude Code, Cursor, Claude Desktop, Codex, or any MCP client.

> "Who are my top 5 customers this month?" · "How many orders were refunded last week?" · "Which products never sold?"

- **No new AI subscription.** db-agent gives your AI safe database tools over [MCP](https://modelcontextprotocol.io). Your AI does the thinking.
- **Read-only and safe.** Every query is checked before it runs. Writes, system tables and secret columns are blocked.
- **Works with any app** that uses MySQL: Laravel, Node, Django, Rails, plain PHP...
- **One small program.** No runtime to install. Your app's code and `.env` are never touched.

---

## 1. Install

Pick one:

```bash
# Node.js users (nothing else to install)
npx -y @db-agent/cli version

# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/frahjokhio/db-agent/main/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/frahjokhio/db-agent/main/install.ps1 | iex
```

> Using npx? Replace `db-agent` with `npx -y @db-agent/cli` in the commands below.

## 2. Create a read-only MySQL user (recommended)

The agent never writes, but a SELECT-only user makes that a hard guarantee from the database itself.

```sql
CREATE USER 'db_agent'@'%' IDENTIFIED BY 'choose-a-strong-password';
GRANT SELECT ON your_database.* TO 'db_agent'@'%';
```

## 3. Connect your database

Run this **in your project folder**:

```bash
db-agent init
```

It asks for:

| Question | Example |
|---|---|
| Project name | `My Shop` |
| MySQL host | `127.0.0.1` |
| MySQL port | `3306` |
| Database name | `my_shop` |
| Username | `db_agent` |
| Password | *(hidden while typing)* |

Settings are saved to `.db-agent/config.env` in your project, and the connection is tested.

Add these lines to your project's `.gitignore` (password + local data):

```
.db-agent/config.env
.db-agent/*.db
```

## 4. Connect your AI tool

```bash
db-agent mcp-config
```

This prints ready-to-paste setup for your tool. For example:

**Claude Code**: run the printed command, e.g.
```bash
claude mcp add db-agent -- db-agent mcp --dir "/path/to/your/project"
```

**Cursor**: `.cursor/mcp.json` · **Claude Desktop**: `claude_desktop_config.json`
```json
{
  "mcpServers": {
    "db-agent": {
      "command": "db-agent",
      "args": ["mcp", "--dir", "/path/to/your/project"]
    }
  }
}
```

**Codex**: `~/.codex/config.toml`
```toml
[mcp_servers.db-agent]
command = "db-agent"
args = ["mcp", "--dir", "/path/to/your/project"]
```

Restart your AI tool. You should see **db-agent** with 6 tools.

## 5. Ask questions

Just ask in your AI chat:

- *"What tables do we have and how are they related?"*
- *"How many new users signed up each week this month?"*
- *"Top 10 products by revenue in 2026"*
- *"Customers who registered but never ordered"*

Answers come back as short bullet points. Ask *"show me the SQL"* any time.

---

## Business rules (optional, recommended)

Your database doesn't know what "revenue" or "active user" means in **your** business. Tell it once:

```bash
db-agent rules init      # creates .db-agent/rules.yaml with suggestions for your schema
db-agent rules           # checks the file against your database
```

```yaml
timezone: Asia/Karachi

definitions:
  active member: A row in members with status = 'active' and expires_at in the future.

notes:
  - Amounts are in USD.

relationships:            # confirm links that have no foreign key
  - order_items.order_id -> orders.id

hidden:
  tables: [audit_logs]    # never shown to the AI
  columns: [users.email]

tables:
  orders:
    description: One row per checkout.
    columns:
      status: "'paid' = money received; 'refunded' = money returned."
```

Changes apply within a minute. `rules.yaml` has no secrets, so you can commit it.

## Configuration

All settings live in `.db-agent/config.env`. Environment variables with the same name override the file (handy on servers and CI).

| Setting | Default | Meaning |
|---|---|---|
| `DB_AGENT_DB_HOST` | `127.0.0.1` | MySQL host |
| `DB_AGENT_DB_PORT` | `3306` | MySQL port |
| `DB_AGENT_DB_NAME` | *(required)* | Database name |
| `DB_AGENT_DB_USER` | *(required)* | MySQL user (read-only recommended) |
| `DB_AGENT_DB_PASSWORD` | *(empty)* | MySQL password |
| `DB_AGENT_PROJECT_NAME` | `My Project` | Name shown to the AI |
| `DB_AGENT_MAX_ROWS` | `100` | Max rows per query (1–10000) |
| `DB_AGENT_QUERY_TIMEOUT_SECONDS` | `30` | Stop slow queries after this many seconds |

**Laravel tip:** use the same host, port and database as `DB_HOST`, `DB_PORT`, `DB_DATABASE` in your Laravel `.env`.

## What is blocked

- Anything except a single `SELECT`: no `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `SET`, `CALL`...
- Other databases and system tables (`mysql.*`, `information_schema`...)
- Secret-looking columns (`password`, `token`, `secret`, `api_key`...) and anything you hide in `rules.yaml`
- Slow or abusive functions (`SLEEP`, `BENCHMARK`, `LOAD_FILE`...)
- Results are capped (`MAX_ROWS`) and queries time out (`QUERY_TIMEOUT_SECONDS`)

Every query (allowed or blocked) is logged on your machine. See it with `db-agent history`.

## Commands

| Command | What it does |
|---|---|
| `db-agent` | Status and next steps |
| `db-agent init` | Connect a database (creates `.db-agent/config.env`) |
| `db-agent check` | Test the connection |
| `db-agent schema [table]` | Show tables, columns and links |
| `db-agent rules` / `rules init` | Check / create business rules |
| `db-agent query "SELECT ..."` | Run a query with the same safety checks |
| `db-agent history` | Recent queries (ok, blocked, failed) |
| `db-agent mcp-config` | Print setup for your AI tool |
| `db-agent mcp` | The MCP server (your AI tool starts it) |

Add `--dir <project folder>` to any command to use another project.

## Troubleshooting

| Problem | Fix |
|---|---|
| `db-agent mcp` shows nothing | Normal: it waits for your AI tool. Press Ctrl+C. |
| AI tool shows db-agent as failed | Run `db-agent check` in the project folder. |
| `Access denied` | Wrong user/password, or the user lacks `SELECT` on the database. |
| `Can't connect` | Check host/port. MySQL in Docker: use the published port. |
| Windows + npx in AI tools | Use the config from `db-agent mcp-config` (it adds `cmd /c` for you). |

**Requirements:** MySQL 5.7.8+ or 8.x. (MariaDB is not supported yet.) Node.js 16+ only if you install with npx.

## Updating and uninstalling

- **npx:** always runs the latest version.
- **Script install:** run the install command again.
- **Uninstall:** delete the `db-agent` binary and your project's `.db-agent/` folder.

## Feedback and contributions

Found a bug or have an idea? [Open an issue](https://github.com/frahjokhio/db-agent/issues).
Want to build on it? Fork the repository and work in your fork.

---

MIT License · Built with Go · [npm](https://www.npmjs.com/package/@db-agent/cli) · [GitHub](https://github.com/frahjokhio/db-agent)

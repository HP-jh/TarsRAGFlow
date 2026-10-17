# TarsRAGFlow

TSG (TarsSecureGuard) MCP adapter for **RAGFlow** — Open-source RAG engine.

This is one of the **15 MCP integration projects** under github.com/HP-jh/, designed to be:

- **One-click downloadable** from TSG's MCP Marketplace
- **One-click enableable** in the TSG gateway
- **Independently versioned** (this repo) with its own release cycle
- **Tightly integrated** with TSG via the standard MCP protocol

## Quick Start

### Standalone

```bash
# Download from GitHub Releases
curl -L -o tars-ragflow https://github.com/HP-jh/TarsRAGFlow/releases/latest/download/tars-ragflow-$(uname -s | tr A-Z a-z)-amd64
chmod +x tars-ragflow
./tars-ragflow --upstream-endpoint <your-RAGFlow-endpoint>
```

### Via TSG MCP Marketplace

1. Open TSG admin → **MCP Marketplace**
2. Find **TarsRAGFlow** in the list
3. Click **Download** → wait for completion
4. Click **Enable** → enter your RAGFlow endpoint
5. The MCP server is now available to all TSG clients

## MCP Tools Exposed

| Tool | Description |
|------|-------------|
| `ragflow_list_models` | List available RAGFlow models |
| `ragflow_invoke` | Invoke RAGFlow with prompt + params |
| `ragflow_health` | Health check of upstream connection |
| `ragflow_status` | Current TSG integration status |

## Configuration

Configurable via env vars or `~/.tars/ragflow/config.json`:

- `TARS_RAGFLOW_ENDPOINT` — upstream endpoint URL
- `TARS_RAGFLOW_API_KEY` — upstream API key (optional, for hosted services)
- `TARS_RAGFLOW_TIMEOUT` — request timeout (default 30s)
- `TARS_RAGFLOW_LOG_LEVEL` — log verbosity (default info)

## Version

- **v0.1.0** — Initial MVP (basic MCP adapter)
- See [Releases](https://github.com/HP-jh/TarsRAGFlow/releases) for full changelog

## Cross-Repo Coordination

This repo is part of the **15 MCP integration projects** in github.com/HP-jh/. See [HP-jh/TarsSecureGuard](https://github.com/HP-jh/TarsSecureGuard) for the master gateway that orchestrates all 15.

## License

MIT — see [LICENSE](LICENSE).

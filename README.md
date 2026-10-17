# TarsRAGFlow

MCP integration for RAGFlow (open source RAG engine).

## Tools

- `ragflow_datasets` — List knowledge base datasets
- `ragflow_chat` — Chat with a RAGFlow knowledge base

## Quick Start

```bash
go build -o tarsragflow
TARS_MCP_ENDPOINT=https://api.example.com TARS_MCP_API_KEY=xxx ./tarsragflow
```

## Environment Variables

- `TARS_MCP_ENDPOINT` — upstream API endpoint
- `TARS_MCP_API_KEY` — upstream API key
- `TARS_MCP_MODE` — `stdio` (default) or `http`

## License

MIT

# gspark

A small, dependency-free Go tool for **offline retrieval over a Markdown knowledge vault** —
plus an **MCP server** so an LLM agent can search that vault locally, with no cloud and no
external services.

Named after **343 Guilty Spark** — the Monitor that watches over an installation.

## Why

A large, interconnected note vault is the knowledge substrate an LLM-driven system reads to do
its work. gspark makes that substrate **searchable, measurable, and portable**:

- **Offline & portable** — a single static Go binary (cross-compiles to Linux / macOS /
  Windows). No Python, no server, no network, no embeddings download.
- **RAG without the cloud** — BM25 lexical retrieval good enough to feed an LLM as context,
  running entirely on the machine.
- **Measurable** — a built-in evaluation harness scores how well the vault answers a fixed exam,
  so you can compare retrieval engines objectively.
- **MCP-native** — exposes `retrieve` / `reindex` over stdio (Model Context Protocol), so any
  MCP client (e.g. an AI coding agent) can query the vault as a tool.

## Commands

```sh
go build -o gspark .

# Retrieve: top notes for a query
gspark search ejemplo-vault "difference between containers and virtual machines"

# Evaluate retrieval against the exam (query -> expected note), swappable engine
gspark eval ejemplo-vault --engine bm25
gspark eval ejemplo-vault --engine hybrid   # BM25 + lexical-semantic expansion

# Validate that every exam target exists in the vault
gspark validate ejemplo-vault

# Run as an MCP server over stdio
gspark mcp ejemplo-vault
```

### Engines are swappable, the exam is fixed

The evaluation harness is the measuring stick; the retrieval engine is a plug. Because the exam
(`eval/queries.json`) never changes, `--engine keyword` vs `bm25` vs `hybrid` is a directly
comparable, reproducible score.

### Pointer, not data (operational queries)

For operational-class queries (IP / host / credential), `search` deliberately returns a
**pointer** to where that data lives for the active entity (`contexto/<entidad>.json`) — never
the value itself. The index stores knowledge, not secrets; it cannot leak what it never holds.
Configure with `--entidad <name>` and a `contexto/<name>.json` (see `contexto/default.json`).

## Layout

```
gspark/
├── main.go         eval / search / validate command dispatch + scoring
├── retrieval.go    index + engines (keyword, BM25)
├── expand.go       hybrid engine: BM25 + lexical-semantic query expansion
├── router.go       pointer-not-data pre-filter for operational queries
├── mcp.go          MCP server (retrieve / reindex) over stdio
├── contexto/       per-entity operational pointers (never the data)
├── eval/           the retrieval exam (queries.json)
└── ejemplo-vault/  tiny demo vault so the commands run out of the box
```

## License

All rights reserved (source-available) — see [LICENSE](LICENSE). Built by Zuriel Vázquez.

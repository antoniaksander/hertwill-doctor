# Hertwill Doctor: guide for AI agents

Read this before running anything. It applies to Claude Code, Codex and any other agent.

## What this repo is

`hwd` is a Go CLI that connects a store's Hertwill dropshipping account to its WooCommerce shop. Agents use it to:

- read the Hertwill import list and catalog
- add products to the import list and sync them to WooCommerce at a set price
- set a WooCommerce product's categories, brand and status (publish)
- diagnose why a product looks wrong in the shop

It replaces the `@hertwill/mcp` write tools, which can't sync products with variants, and the WooCommerce MCP update tool, which currently fails. Their read tools are fine to use alongside `hwd`.

The tool is generic: no client-specific logic. Pricing rules, categories and brands come from the user, per task.

## Setup and which store

- Build: `go build -o hwd ./cmd/hwd` (`hwd.exe` on Windows). Tests: `go test ./...`.
- Config comes from `.env` in the current directory, or from the file in `HWD_ENV_FILE`. One file per store; see `docs/setup.md`.
- **Always run `hwd doctor` first** and tell the user which store it shows (`Store:` line). If it is not the store the user means, stop.
- **Never read, print or edit `.env` files** or any key. Don't type keys into commands. If config is missing, tell the user which variable to set.

## Safety rules

- Every write needs `--dry-run` or `--confirm`. Run the dry run, show the result, and get the user's OK before every `--confirm`.
- Prices are **absolute selling prices** including VAT (`--price 40.95`), never a markup multiplier. Hertwill refuses prices below cost.
- Synced products land in WooCommerce as **private**. Publish only when the user says so, with `hwd woo set-status`.
- **Don't re-sync a product that is already live** without telling the user: a re-sync sets it to private first, and if the sync fails it stays hidden. Note its status and categories before, check them after, and restore with `set-status` / `set-terms`.
- 403 "You are not allowed to sync this product" is blocked on Hertwill's side. Report it; don't retry.
- Don't change content (titles, descriptions, images) unless asked.

## Workflow

The full step-by-step workflow is in `.claude/skills/hertwill-sync/SKILL.md`. In short:

1. `hwd doctor`, then `hwd hertwill import-list` (`--contains <text>` to filter).
2. Agree prices with the user. Check competitor prices if asked; show cost, price and profit in a table.
3. Write a price file (`<id> <price>` per line), `hwd hertwill sync --file <path> --dry-run`, then `--confirm`.
4. `hwd woo set-terms` for categories and brand (copy IDs from a similar product), then report.
5. Publish with `hwd woo set-status --status publish` only after the user approves the table.

Keep reports short: one table with name, price, status, and any failures with Hertwill's message.

## Code conventions

- Commands live in `internal/cli`, API clients in `internal/hertwill` and `internal/woocommerce`.
- New write commands follow the existing pattern: `--dry-run`/`--confirm`, send only the fields being changed, tests with a fake server (`httptest`).
- Update `README.md`, `docs/commands.md` and `docs/hertwill-endpoints.md` with any new command or endpoint finding.

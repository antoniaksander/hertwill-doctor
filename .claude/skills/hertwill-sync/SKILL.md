---
name: hertwill-sync
description: Import and sync Hertwill dropshipping products to the connected WooCommerce store with the hwd CLI. Use when the user asks to import, sync, price or check Hertwill products (e.g. "sync everything in my import list", "import the Breden hats"), or asks why a Hertwill sync failed.
---

# Hertwill sync with hwd

`hwd` (Hertwill Doctor) talks to the Hertwill API directly. Use it instead of the `@hertwill/mcp` write tools, which can't sync products with variants.

## Setup facts

- Run every command from the hertwill-doctor directory, because `hwd` loads `.env` from the current directory. The directory is in the `HWD_DIR` environment variable; if it's unset, ask the user where hertwill-doctor is.
  - macOS/Linux: `cd "$HWD_DIR" && ./hwd ...`
  - Windows PowerShell: `Set-Location $env:HWD_DIR; .\hwd.exe ...`
- Never read, print or edit `.env`. It holds the Hertwill key and WooCommerce credentials. Check config with `hwd doctor`.
- If `hwd` is missing, build it: `go build -o hwd ./cmd/hwd` (`hwd.exe` on Windows).

## How Hertwill behaves

- Catalog product ID (e.g. 9107) is what every command takes.
- `hwd hertwill import-list` shows only products **not synced yet**. `--status synced` lists synced ones. Synced products drop out of the default list.
- **Price is absolute.** `--price 40.95` sells at €40.95. Never pass a markup multiplier; Hertwill rejects prices below cost.
- Each product and all its variations get the same price.
- Synced products land in WooCommerce as **private**, in the "All" category, without brand. Nothing in this workflow publishes them.
- Set brand and categories with `hwd woo set-terms --id <woo id> --categories <ids> --brands <ids> (--dry-run | --confirm)`. The lists replace the current ones. Copy the IDs from a similar product that is already set up (`hwd --json woo product --id <id>` shows names with IDs). The WooCommerce MCP update tool currently fails with an outputSchema error, so don't rely on it for writes.
- **Re-syncing a product that is already in WooCommerce sets it to private first.** If the sync then fails (seen for MIKA, HAPPY, KLAUS: "sync-failed", no error detail), the live product stays hidden. Before re-syncing a live product, note its status and categories, and check them afterwards; restore with `hwd woo set-status` / `set-terms`. `hwd hertwill sync-job --id <id>` shows Hertwill's job status.
- 403 "You are not allowed to sync this product" means Hertwill blocks that product for the store. It has to be fixed in the Hertwill dashboard; don't retry.

## Workflow

1. **See what's there.** `hwd hertwill import-list` (add `--contains <text>` to filter, `--json` for details). Show the user a short table: ID, name, cost, variant count.
2. **Add missing products** if the user named ones not in the list: `hwd hertwill import --ids <ids> --dry-run`, show the result, then `--confirm` after the user agrees.
3. **Agree on prices.** The user decides selling prices. If they give a rule (e.g. "cost × 2, rounded up to .95"), compute each price, show a table of ID, name, cost and price, and get approval. Never invent prices.
4. **Write a price file** in a temp/scratch location, one `<id> <price>` per line.
5. **Dry run:** `hwd hertwill sync --file <path> --dry-run`. Fix anything refused.
6. **Test one product first** on a new batch: `hwd hertwill sync --id <id> --price <price> --confirm`, then check it in WooCommerce (WooCommerce MCP or `hwd woo product --sku <sku>`): price and status.
7. **Sync the rest** after the user says go: `hwd hertwill sync --file <path> --confirm --json`.
8. **Brand and categories**, if the user asked: dry run, then `--confirm`.
9. **Publish** only when the user asks: `hwd woo set-status --id <woo id> --status publish --dry-run`, then `--confirm`.
10. **Report** one table from WooCommerce: name, price, status. List failures with Hertwill's error message. Don't publish or edit products in WooCommerce unless the user asks.

Ask before every `--confirm` run. Dry runs and read commands are fine without asking.

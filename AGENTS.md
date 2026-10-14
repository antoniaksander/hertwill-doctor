# Hertwill Doctor: guide for AI agents

Read this before running anything. It applies to Claude Code, Codex and any other agent.

## What this repo is

`hwd` is a Go CLI that connects a store's Hertwill dropshipping account to its WooCommerce shop. Agents use it to:

- read the Hertwill import list and catalog
- add products to the import list and sync them to WooCommerce at a set price
- set a WooCommerce product's categories, brand, shipping class and status (publish)
- read brands' delivery rates for shipping classes
- diagnose why a product looks wrong in the shop

It replaces the `@hertwill/mcp` write tools, which can't sync products with variants, and the WooCommerce MCP update tool, which currently fails. Their read tools are fine to use alongside `hwd`.

The tool is generic: no client-specific logic. Pricing rules, categories and brands come from the user, per task.

## Starting from zero context

If the user only points you at this repo (URL or folder) and gives no other context:

1. Find the local copy: `$HWD_DIR` (Windows: `$env:HWD_DIR`), or ask where it is. If there is none, offer to clone and build it, and point the user to `docs/setup.md` to create their `.env` themselves.
2. Build `hwd` if it's missing, then run `hwd doctor` and report the `Store:` line and whether both APIs are configured.
3. Ask what they want to do. Don't start syncing or changing anything on your own.

## Setup and which store

- Build: `go build -o hwd ./cmd/hwd` (`hwd.exe` on Windows). Tests: `go test ./...`.
- Config comes from `.env` in the current directory, or from the file in `HWD_ENV_FILE`. One file per store; see `docs/setup.md`.
- **Always run `hwd doctor` first** and tell the user which store it shows (`Store:` line). If it is not the store the user means, stop.
- **Never read, print or edit `.env` files** or any key. Don't type keys into commands. If config is missing, tell the user which variable to set.

## Before proposing prices

Ask the user for anything the task doesn't already say. Don't assume:

- **VAT rate** the selling price includes, and whether the Hertwill cost is excl. VAT (it is).
- **Discount codes** customers usually use (e.g. a 10% welcome code), so profit is shown with and without them.
- **Price-ending style**: e.g. match the brand's own shop (.90, .95, .50), or a fixed ending. Check the brand's shop if unsure.
- **Which products to stay in line with**: usually the same brand's products already in the shop.

Compare **in-stock** listings only and give links. Show cost, proposed price, and profit at full price and after the usual discount. Profit excludes shipping and payment fees; say so.

## Shipping classes on import

Hertwill charges the store each brand's delivery fee (per order, or per item for bulky brands). If the store passes these on with WooCommerce shipping classes (ask the user, or check whether products of the same brand already have one):

1. **Check** the brand's class: `hwd woo product --sku <sku of a live product of the same brand>` shows its `Shipping class`.
2. **If the brand has no class yet**, get its rates with `hwd hertwill brand-shipping --brands <brand ID>` (brand ID: `hwd hertwill product --id <ID> --raw`, `data.brand.id`). Propose the class and its per-country costs to the user, and create it only after their OK (WooCommerce › Settings › Shipping › Classes, then the class cost in each zone's Flat rate). Countries without a price are not deliverable; enter them as the store's "not deliverable" cost if it has one.
3. **Per-item brands** with several lists: each product needs the class of its own list (`shipping_price_id` on the product's hertwill.com page).
4. **After syncing**, give every new product its class: `hwd woo set-shipping-class --id <ID> --class <slug> --dry-run`, then `--confirm`. Synced products arrive without one.
5. Report products that ship to fewer countries than the store sells to.

The class naming, fee formula and zones are store-specific: follow the user's rules or the store's own docs.

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
4. `hwd woo set-terms` for categories and brand (copy IDs from a similar product), and `hwd woo set-shipping-class` if the store uses brand classes (see above), then report.
5. Publish with `hwd woo set-status --status publish` only after the user approves the table.

Keep reports short: one table with name, price, status, and any failures with Hertwill's message.

## Code conventions

- Commands live in `internal/cli`, API clients in `internal/hertwill` and `internal/woocommerce`.
- New write commands follow the existing pattern: `--dry-run`/`--confirm`, send only the fields being changed, tests with a fake server (`httptest`).
- Update `README.md`, `docs/commands.md` and `docs/hertwill-endpoints.md` with any new command or endpoint finding.

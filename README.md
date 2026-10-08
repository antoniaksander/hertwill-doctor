# Hertwill Doctor

**Manage your Hertwill dropshipping products in WooCommerce from the command line, or let an AI agent (Claude Code, Codex) do it for you.**

If you sell [Hertwill](https://hertwill.com) products in a WooCommerce shop, `hwd` lets you:

- see your Hertwill import list, with costs and variants
- add products to the import list and sync them to your shop at the price you choose
- set categories, brand and publish status in WooCommerce
- find out why a product looks wrong (missing images, wrong price, failed sync)

It talks to the Hertwill and WooCommerce APIs directly. Every change needs an explicit `--dry-run` or `--confirm`, prices are always the final selling price, and synced products stay private until you publish them.

## Quick start

1. You need Go 1.22+, a Hertwill API key, and a WooCommerce REST API key with Read/Write access.
2. Build and configure:

   ```sh
   git clone https://github.com/antoniaksander/hertwill-doctor.git
   cd hertwill-doctor
   go build -o hwd ./cmd/hwd
   cp .env.example .env    # fill in your store name and keys
   ./hwd doctor
   ./hwd hertwill import-list
   ```

3. Sync a product at your price, check it, then publish:

   ```sh
   ./hwd hertwill sync --id 9107 --price 40.95 --dry-run
   ./hwd hertwill sync --id 9107 --price 40.95 --confirm
   ./hwd woo product --sku <sku>
   ./hwd woo set-status --id <woo id> --status publish --confirm
   ```

Full setup, including several stores and Windows: [docs/setup.md](docs/setup.md).

## Use it with an AI agent

Start a new Claude Code or Codex chat with no context and just point it at this repo:

```text
Use https://github.com/antoniaksander/hertwill-doctor to manage my Hertwill products in WooCommerce.
My local copy is in $HWD_DIR. Read its AGENTS.md first, run hwd doctor, then wait for my task.
```

The agent reads [AGENTS.md](AGENTS.md) (scope, safety rules, workflow), checks which store it is connected to, and asks before every change. For a longer version with options, see [docs/starting-prompt.md](docs/starting-prompt.md). Claude Code users can also install the `hertwill-sync` skill (see setup).

Then ask in plain words, for example: *"Show the Breden products in my import list, check competitor prices and propose profitable prices. Keep them private until I approve."*

## What it can't do (yet)

- Re-syncing a product that is already live can fail on Hertwill's side and leaves it private; `hwd` warns about this but can't fix it.
- Products Hertwill hasn't approved for your store return 403 and must be unblocked in Hertwill.
- No order handling: `hwd` covers products only.

---

# Reference

## Install / Build

Development build:

```sh
go build -o hwd ./cmd/hwd
```

Release-style build with version metadata:

```sh
go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o hwd ./cmd/hwd
```

## Version

```sh
hwd version
```

Example output:

```text
Version: 0.1.0
Commit: abc1234
Date: 2026-07-07T12:00:00Z
```

JSON output:

```sh
hwd version --json
```

## Configuration

Configuration precedence:

1. command flags, where available
2. environment variables
3. `.env` file
4. built-in defaults

Environment variables override `.env` values. Set `HWD_ENV_FILE` to read a different file, for example one per store (see [docs/setup.md](docs/setup.md)); `HWD_STORE_NAME` labels the store in `hwd doctor`.

WooCommerce configuration is valid only if all are present:

- `WOO_BASE_URL`
- `WOO_CONSUMER_KEY`
- `WOO_CONSUMER_SECRET`

Hertwill configuration is valid if either `HERTWILL_ACCESS_TOKEN` is present, or both `HERTWILL_EMAIL` and `HERTWILL_PASSWORD` are present.

For v1, static access token authentication is supported cleanly. If email/password credentials are provided, Hertwill Doctor returns:

```text
Hertwill email/password authentication endpoint is not verified yet. Provide HERTWILL_ACCESS_TOKEN or verify the auth endpoint in docs/hertwill-endpoints.md.
```

Timeouts are controlled by:

```sh
HWD_TIMEOUT_SECONDS=30
```

The value must be a positive integer. Missing values default to `30`; invalid values fall back to `30` and are reported by `hwd doctor`.

## Commands

```sh
hwd doctor
hwd woo product --sku ABC123
hwd woo product --id 123
hwd woo search --query "boots" --limit 10
hwd hertwill list --limit 10
hwd hertwill list --limit 100 --contains "Moomin"
hwd hertwill list --limit 10 --debug --raw
hwd hertwill search --query "boots" --limit 10
hwd hertwill search --query MOOMIN42B --raw
hwd hertwill product --id 123
hwd hertwill sync-status --id 123
hwd hertwill import-list --contains "breden"
hwd hertwill import --ids 8096,8097 --dry-run
hwd hertwill sync --id 9107 --price 40.95 --dry-run
hwd hertwill sync --file prices.txt --confirm
hwd diagnose --sku ABC123
hwd diagnose --sku ABC123 --no-hertwill-search
hwd diagnose --sku ABC123 --hertwill-id 123
hwd compare --sku ABC123 --hertwill-id 123
hwd woo repair-images --sku ABC123 --hertwill-id 123 --dry-run
hwd woo set-terms --id 31968 --categories 54,765 --brands 780 --dry-run
hwd woo set-status --id 31968 --status publish --dry-run
hwd woo set-price --id 9415 --price 32.50 --dry-run
hwd woo set-name --id 1636 --name "New name" --dry-run
hwd woo trash --id 9427 --dry-run
hwd woo replace --id 1636 --field short_description --find Old --replace New --dry-run
hwd woo repair-images --sku ABC123 --hertwill-id 123 --confirm
hwd woo repair-images --sku ABC123 --hertwill-id 123 --max-images 1 --confirm
```

Search commands default to `--limit 10` and enforce a maximum of `100`. Search output may be truncated by the default limit.

WooCommerce search maps `--limit` to the WooCommerce REST `per_page` query parameter. Hertwill search uses the documented `GET /v1/products/search?q=<query>` endpoint and applies `--limit` locally because no Hertwill limit parameter is verified in v1. Hertwill list uses `GET /v1/products`; `--contains` filters locally over the returned API response and may not search the full catalog if the API is paginated.

`hwd woo product` accepts either `--sku` or `--id`. If both are provided, the command returns a clear error. SKU remains the primary sync diagnostic path.

`hwd diagnose --sku <SKU>` checks WooCommerce by SKU and then tries to identify the Hertwill-side product with Hertwill SKU search when Hertwill credentials are configured. If SKU search returns no exact match and the WooCommerce product name is available, diagnose performs a conservative title search and reports possible candidates as ambiguous instead of confirmed matches. Use `--no-hertwill-search` to disable the automatic Hertwill lookup.

`hwd diagnose --hertwill-id <ID>` fetches the Hertwill product by ID but does **not** automatically call Hertwill sync-status. That endpoint is unverified (see `docs/hertwill-endpoints.md`), and calling it by default produced noisy, unactionable 404 warnings. Diagnose will call sync-status automatically only once the endpoint is marked verified.

`hwd hertwill sync-status --id <ID>` remains available but does not call the unverified endpoint. It exits with a non-zero status and prints `Hertwill sync-status endpoint is not verified yet.` instead of pretending success.

`hwd woo repair-images --sku <SKU> --hertwill-id <ID> (--dry-run | --confirm) [--force]` is a narrowly-scoped write for the "Hertwill has images, WooCommerce has none" diagnose case. It replaces only the WooCommerce product's images with Hertwill's image URLs (`PUT /wp-json/wc/v3/products/{id}` with `{"images": [{"src": "..."}]}` only) and never touches price, stock, status, description, or variations, and never publishes a product. Exactly one of `--dry-run`/`--confirm` is required. It refuses to write (both modes exit non-zero, no request is sent) if either product can't be found, Hertwill has no images, or the WooCommerce/Hertwill SKUs both exist but don't match. If the WooCommerce product already has images, or a SKU is missing on one side but names match exactly, `--confirm` requires `--force` (`--dry-run` still previews either way). See `docs/commands.md` for full details and JSON field reference.

**Timeout guidance:** WooCommerce/WordPress sideloads each remote image during the `PUT` request, so sending every Hertwill image at once can time out on stores with many/large images — the update then does not complete even though the request was sent. Use `--max-images <N>` (or `--featured-only`, shorthand for `--max-images 1`) to send only the first N Hertwill images per confirm, or raise `HWD_TIMEOUT_SECONDS=120` for a longer client timeout. Timeouts are never increased automatically. Since each confirm replaces the WooCommerce images array rather than appending to it, applying a larger batch after a smaller one requires `--force`.

Use `--raw` with Hertwill list/search to inspect the exact read-only API response shape:

```sh
hwd hertwill search --query MOOMIN42B --raw --debug
hwd hertwill list --raw --debug
```

Raw mode prints only the response body to stdout. Debug output, sanitized request details, response status/content-type/body byte count, request counters, and rate-limit headers stay on stderr. Authorization headers and full tokens are never printed.

`hwd woo set-terms --id <ID> [--categories <ID,...>] [--brands <ID,...>] (--dry-run | --confirm)` replaces a product's categories and/or brands via `PUT /wp-json/wc/v3/products/{id}`. Only the given fields are sent; a flag that is left out is not changed, and `none` clears a list. Price, stock, status and content are never touched.

`hwd woo set-status --id <ID> --status <publish|private|draft|pending> (--dry-run | --confirm)` changes only the product's status.

`hwd woo set-price (--id <ID> --price <PRICE> | --file <PATH>) (--dry-run | --confirm)` changes only the regular price of simple products (variable products are refused). Use it to reprice products that are already live, instead of re-syncing them.

## Import list and sync

These commands replace the Hertwill MCP write tools, which can't sync products with variants.

```sh
hwd hertwill import-list [--status <STATUS>] [--contains <TEXT>] [--json]
hwd hertwill import --ids <ID,ID,...> (--dry-run | --confirm)
hwd hertwill sync (--id <ID> --price <PRICE> | --file <PATH>) (--dry-run | --confirm)
```

- `import-list` reads every page of `GET /v1/import-list`. Without `--status`, Hertwill returns only products that are not synced yet; `--status synced` lists synced ones. `--json` includes each variation's `dropship_id`.
- `import` adds catalog product IDs (max 50) via `POST /v1/import-list/products`. IDs already in the list are skipped.
- `sync` calls `POST /v1/sync/products` once per product. **`--price` is the absolute selling price** (e.g. `40.95`), not a markup multiplier: Hertwill's `default_store_markup` field is a price, and the API rejects prices below wholesale cost. The same price is sent for the product and every variation, using the variation `dropship_id`s from the import list.
- The price file has one `<id> <price>` per line; `#` comments and blank lines are ignored.
- Products missing from the unsynced import list, or priced below cost, are refused before any request. `--confirm` keeps going after a failed product, then exits non-zero if anything failed or was refused.
- Synced products land in WooCommerce as private. Nothing here publishes them.

`HERTWILL_ACCESS_TOKEN` can be a Hertwill API key (`hw_live_...`).

## Doctor

```sh
hwd doctor
hwd doctor --debug
hwd doctor --json
```

`hwd doctor` separates local configuration checks from live API connectivity checks.

Local checks:

- WooCommerce credential set completeness
- Hertwill credential set completeness
- HTTP timeout configuration

Live checks:

- WooCommerce: performs a safe read-only request to `/wp-json/wc/v3/products?per_page=1` when WooCommerce credentials are complete
- Hertwill: skips live connectivity until a lightweight health/check endpoint is verified

With `--debug`, the WooCommerce live check is included in the request summary as one WooCommerce REST attempt. Hertwill live checks do not increment request counters while no verified lightweight endpoint exists.

## Output

Default output is human-readable tables.

JSON output:

```sh
hwd woo search --query "boots" --limit 10 --json
```

When `--json` is used, stdout contains only valid JSON. Debug output and warnings/errors that are not part of JSON go to stderr.

Disable colored human-readable output:

```sh
hwd doctor --no-color
```

JSON output never contains color codes.

## Debugging

Use:

```sh
hwd compare --sku ABC123 --hertwill-id 123 --debug
```

Debug output is written to stderr and includes sanitized request details plus request counts. Secrets are never printed in full.

Request summary format:

```text
Request summary:
This command:
Hertwill API attempts: X
Hertwill API failures: Y
WooCommerce REST attempts: A
WooCommerce REST failures: B

Configured limits:
Hertwill public endpoints: 60 requests/minute per IP
Hertwill authenticated endpoints: 300 requests/minute per API key
WooCommerce REST: store/server dependent
```

Attempts count all HTTP attempts before or when sending the request for this command. Failures include non-2xx HTTP status codes, network errors, timeout errors, and JSON parse errors where applicable. Hertwill `RateLimit` and `RateLimit-Policy` response headers are shown when present; the tool does not claim remaining requests unless the API provides that data.

## Compare

```sh
hwd compare --sku ABC123 --hertwill-id 123
```

Example:

```text
Field   WooCommerce   Hertwill    Match
Found   yes           yes         yes
Name    Pilot Boots   Pilot Boots yes
SKU     ABC123        ABC123      yes
Price   59.00         59.00       yes
Images  4             6           no
Status  publish       synced      no
```

Only available response fields are compared. Unavailable fields are shown as `n/a`.

## Documentation

- [Setup: new machine, new store, Claude Code and Codex](docs/setup.md)
- [Starting prompt for a new AI chat](docs/starting-prompt.md)
- [Agent guide](AGENTS.md)
- [Commands](docs/commands.md)
- [Configuration](docs/configuration.md)
- [Hertwill endpoints](docs/hertwill-endpoints.md)

## Roadmap

- MySQL checks
- SSH log checks
- Hostinger profile
- Roxder/private profile
- WordPress helper plugin
- safe retry actions
- GitHub Releases binary builds
- Homebrew install support
- YAML output
- CSV output
- spinner/progress indicator
- pagination with `--page`
- optional experimental endpoint probing, but not by default
- order and stock commands

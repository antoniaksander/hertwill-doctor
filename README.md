# Hertwill Doctor

Hertwill Doctor is a generic public Go CLI for diagnosing Hertwill API and WooCommerce sync/import issues.

The CLI command is:

```sh
hwd
```

v1 is read-only. It does not perform write actions, sync retries, product editing, product publishing, product deletion, direct SQL, SSH, Hostinger-specific logic, or Roxder-specific logic.

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

Environment variables override `.env` values.

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
hwd diagnose --sku ABC123
hwd diagnose --sku ABC123 --no-hertwill-search
hwd diagnose --sku ABC123 --hertwill-id 123
hwd compare --sku ABC123 --hertwill-id 123
hwd woo repair-images --sku ABC123 --hertwill-id 123 --dry-run
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
- optional write actions with confirmation
- GitHub Releases binary builds
- Homebrew install support
- YAML output
- CSV output
- spinner/progress indicator
- pagination with `--page`
- optional experimental endpoint probing, but not by default

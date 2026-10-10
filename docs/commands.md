# Commands

Global flags:

- `--json`: write valid JSON to stdout only
- `--debug`: write sanitized request details and request summary to stderr
- `--no-color`: disable colored human-readable output

## Version

```sh
hwd version
hwd version --json
```

## Doctor

```sh
hwd doctor
hwd doctor --json
hwd doctor --debug
```

Performs local configuration checks and verified live API checks.

Local checks:

- WooCommerce credential set completeness
- Hertwill credential set completeness
- HTTP timeout configuration

Live checks:

- WooCommerce: safe read-only request to `/wp-json/wc/v3/products?per_page=1` when WooCommerce credentials are complete
- Hertwill: skipped until a lightweight health/check endpoint is verified

JSON output includes separate `config_checks` and `live_checks` objects. Debug output includes request attempts and failures on stderr.

## WooCommerce

```sh
hwd woo product --sku ABC123
hwd woo product --id 123
hwd woo search --query "boots" --limit 10
```

For `hwd woo product`, provide either `--sku` or `--id`. If both are provided, the command returns an error. SKU remains the primary sync diagnostic path. `--sku <SKU> --trash` looks in the WooCommerce trash instead (the default lookup skips trashed products, but their SKUs still block a new product with the same SKU).

Search defaults to `--limit 10` and enforces a maximum of `100`. Output may be truncated by the default limit.

Future note: pagination may add `--page`.

### WooCommerce image repair

```sh
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --dry-run
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --dry-run --json
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --confirm
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --confirm --force
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --max-images 1 --dry-run
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --featured-only --dry-run
hwd woo repair-images --sku MOOMIN42B --hertwill-id 3912 --max-images 3 --confirm --force
```

`hwd woo repair-images` is a narrowly-scoped write command for the case where diagnose reports "Hertwill source product has images, but WooCommerce product has none." It replaces only the WooCommerce product's `images` field with Hertwill's image URLs via `PUT /wp-json/wc/v3/products/{id}` with a body of `{"images": [{"src": "..."}, ...]}`. It never sends or changes price, stock, status, description, or variations, and never publishes a product.

Exactly one of `--dry-run` or `--confirm` is required; providing both or neither is an error. `--dry-run` fetches both products, runs all safety checks, and reports what would be sent, without making any WooCommerce write. `--confirm` performs the same checks and then issues the write, followed by a fresh `GET` re-fetch of the WooCommerce product to confirm and report the before/after image count.

### Batching with `--max-images` / `--featured-only`

WooCommerce/WordPress sideloads (downloads and imports) each remote image during the `PUT` request. Applying every Hertwill image at once can exceed hosting/PHP/API request timeouts on stores with many or large images — the request times out and the image update does not complete (WooCommerce image count stays unchanged), even though the `PUT` was sent.

- `--max-images <N>` limits the images sent to the first `N` deduplicated Hertwill image URLs, in Hertwill's order (featured image first, then gallery). `N` must be `>= 1`; `--max-images 0` or a negative value refuses.
- `--featured-only` is shorthand for `--max-images 1`.
- Providing both `--max-images` and `--featured-only` refuses.

If a `--confirm` run times out, retry with a smaller batch (`--max-images 1` or `--featured-only`), or raise the client timeout with `HWD_TIMEOUT_SECONDS=120`. The client timeout is never increased automatically.

Dry-run output with a limit reports `Hertwill images total`, `Image limit`, `Images to apply`, and a note ("Only the first N Hertwill image(s) will be applied."). JSON output adds `image_limit`, `hertwill_image_count_total`, and `images_to_apply_count` alongside the existing `images_to_apply` list.

Because each batch write replaces the WooCommerce `images` array (not appends to it), applying a small batch first and a larger batch later is expected to require `--force` the second time, since WooCommerce will already have images from the first batch — `--force` replaces the existing WooCommerce featured/gallery images with the newly selected Hertwill image set.

The command refuses to run (both `--dry-run` and `--confirm` exit non-zero and no write is attempted) when:

- the WooCommerce product cannot be found by SKU, or its ID is empty
- the Hertwill product cannot be found by ID
- the Hertwill product has no images to apply
- WooCommerce and Hertwill SKUs are both present but do not match
- SKU is missing on at least one side and the WooCommerce/Hertwill product names do not match exactly

If names differ but SKUs match, the command proceeds and adds a warning rather than refusing. If a SKU is missing on one side but the names match exactly, `--dry-run` still previews the repair, but `--confirm` requires `--force`. Similarly, if the WooCommerce product already has images, `--confirm` requires `--force` to overwrite them (`--dry-run` always previews regardless).

JSON output (`--json`) includes `mode`, `changed`, `woocommerce_before`, `woocommerce_after` (confirm only), `hertwill`, `image_limit`, `hertwill_image_count_total`, `images_to_apply`, `images_to_apply_count`, `warnings`, `refused_reason` (when refused), and `request_summary`.

Debug output (`--debug`) never dumps the full list of image URLs being sent in the `PUT` body — for the image update request it logs `WooCommerce PUT image count: N` instead, to keep debug output readable for large batches. The full URL list is still available in normal dry-run output and JSON.

## WooCommerce categories and brands

```sh
hwd woo categories
hwd woo categories --contains beauty
hwd woo brands --contains fox --json
```

Read-only. `categories` reads every page of `GET /wp-json/wc/v3/products/categories` and prints the tree with ID, name and product count, children indented under their parent. `brands` does the same for `GET /wp-json/wc/v3/products/brands`. `--contains` keeps matching terms plus their parents, so each match shows its full path. `--json` adds each term's `path` (e.g. `Women > Women Beauty products > Women Creams & Scrubs`). Use the IDs with `woo set-terms`. Product counts are WooCommerce's own and only include published products.

## WooCommerce product edits

Every edit needs exactly one of `--dry-run` or `--confirm`, sends only the field(s) it changes, and never publishes unless you use `set-status`.

```sh
hwd woo set-terms --id 31968 --categories 54,765 --brands 780 --dry-run
hwd woo set-status --id 31968 --status publish --dry-run
hwd woo set-price --id 9415 --price 32.50 --dry-run
hwd woo set-price --file prices.txt --confirm
hwd woo set-name --id 1636 --name "New name" --dry-run
hwd woo replace --id 1636 --field short_description --find Old --replace New --dry-run
hwd woo trash --id 9427 --dry-run
hwd woo create-category --name "Stroller Accessories" --parent 283 --dry-run
```

- `set-terms` replaces a product's categories and/or brands. A flag that is left out is not changed; `none` clears a list.
- `set-status` changes only the status: `publish`, `private`, `draft` or `pending`.
- `set-price` changes only the regular price incl. VAT. For a variable product it sets the same price on every variation (`POST /products/{id}/variations/batch`, 100 per request). The file has one `<woo id> <price>` per line. Sale prices are reported, not changed. Use it to reprice live products; `hwd hertwill sync` refuses products that are already synced. A later Hertwill re-sync may set its own price again.
- `set-name` changes the title; the URL slug stays.
- `replace` does a case-sensitive find and replace in one of `name`, `slug`, `short_description` or `description`, and refuses if the text isn't found.
- `trash` moves a product to the bin; it is never deleted permanently. Remove it from the Hertwill import list too, or a later sync may create it again.
- `create-category` refuses if a category with the same name already exists.

## Hertwill

```sh
hwd hertwill list --limit 10
hwd hertwill list --limit 100 --contains "Moomin"
hwd hertwill list --raw
hwd hertwill search --query "boots" --limit 10
hwd hertwill search --query MOOMIN42B --raw
hwd hertwill product --id 123
hwd hertwill sync-status --id 123
```

Search uses `GET /v1/products/search?q=<query>`. `--limit` defaults to `10`, enforces a maximum of `100`, and is applied locally because no Hertwill limit parameter is verified in v1. Output may be truncated by the default limit.

List uses `GET /v1/products`. `--contains` applies a local case-insensitive filter over products returned by that API response only; it may not search the entire Hertwill catalog if the API is paginated.

`--raw` prints the raw Hertwill JSON response body for list/search. Use it with `--debug` to inspect response shape while keeping sanitized request details, status, content type, body byte count, request counters, and rate-limit headers on stderr.

If no results are returned, human-readable output says `No results`.

Future note: pagination may add `--page`.

`hwd hertwill sync-status --id <ID>` does not call `/v1/products/{id}/sync-status`; that endpoint is unverified (see `docs/hertwill-endpoints.md`). The command exits with a non-zero status code and prints `Hertwill sync-status endpoint is not verified yet.` on stderr instead of making the request or pretending success. Once the endpoint is verified in `internal/hertwill/endpoints.go`, this command will call it normally.

## Hertwill import list and sync

```sh
hwd hertwill import-list
hwd hertwill import-list --status synced --json
hwd hertwill import-list --contains "breden"
hwd hertwill import --ids 8096,8097 --dry-run
hwd hertwill import --ids 8096,8097 --confirm
hwd hertwill sync --id 9107 --price 40.95 --dry-run
hwd hertwill sync --file prices.txt --confirm --json
hwd hertwill sync --id 811 --price 79.95 --retry-failed --dry-run
hwd hertwill remove --ids 811 --dry-run
```

`import-list` fetches every page of `GET /v1/import-list` (20 per page). Without `--status`, Hertwill returns only products that are not synced yet. Columns: catalog ID, name, import status, wholesale cost, variant count, SKU. `--json` adds `product_id` (store dropship ID) and each variation's `id` and `dropship_id`.

`import` requires exactly one of `--dry-run`/`--confirm`. It reads the import list first and only posts IDs that are not already in it. Hertwill answers per ID with `added`, `already_exists`, `not_found` or `error`.

`sync` requires exactly one of `--dry-run`/`--confirm`, and either `--id` with `--price` or `--file`. `--price` is the absolute selling price in `--currency` (default `EUR`). Each request body is:

```json
{"product_id": 9107, "default_store_markup": 40.95, "currency": "EUR",
 "variations": [{"id": 20610, "dropship_id": 3253249, "default_store_markup": 40.95}]}
```

Refused locally (no request sent): product not in the unsynced import list, or price below its wholesale cost. With `--confirm`, requests are sent one at a time with `--delay-ms` (default 1000) between them; a failed product is reported with Hertwill's error code and message, and the command continues. Exit code is non-zero if any product failed or was refused. JSON output includes `entries` (id, name, cost, price, variations, result, error, refused_reason) and `started`/`failed`/`refused` counts.

`--retry-failed` also accepts products with import status `sync-failed`, which drop out of the default list. If such a product is live in WooCommerce, the re-sync sets it to private first.

`remove` deletes products from the import list (`DELETE /v1/import-list/products/{id}`, one request per ID, at most 50). It refuses any product whose SKU is in WooCommerce, because removing may unlink a live product; `--force` overrides.

### Broken WooCommerce link

Sometimes Hertwill keeps a link to a WooCommerce product that no longer exists. Every sync then either fails or reports `synced` without creating anything, and the product never appears in the shop. To fix it, remove the product from the import list, add it again, and sync it:

```sh
hwd hertwill remove --ids 811 --confirm
hwd hertwill import --ids 811 --confirm
hwd hertwill sync --id 811 --price 79.95 --confirm
```

## Diagnose

```sh
hwd diagnose --sku ABC123
hwd diagnose --sku ABC123 --no-hertwill-search
hwd diagnose --sku ABC123 --hertwill-id 123
```

Without `--hertwill-id`, diagnose looks up the WooCommerce product by SKU and then tries Hertwill product search by SKU when Hertwill credentials are configured. If SKU search has no exact match and WooCommerce returned a product name, diagnose performs a title search and reports candidates as ambiguous. `--no-hertwill-search` disables the automatic Hertwill lookup. With `--hertwill-id`, diagnose uses that ID and does not auto-search.

With `--hertwill-id`, diagnose fetches the Hertwill product by ID only. It does not automatically call Hertwill sync-status, because that endpoint is unverified; calling it by default produced noisy 404 warnings on every run. Diagnose will call sync-status automatically once the endpoint is verified in `internal/hertwill/endpoints.go`.

JSON output includes Hertwill search metadata under `hertwill.searches` and rate-limit headers under `request_summary.rate_limit_info` when Hertwill sends them.

## Compare

```sh
hwd compare --sku ABC123 --hertwill-id 123
```

Compares available WooCommerce and Hertwill fields side by side. Unavailable fields are shown as `n/a`.

## Debug output

`--debug` writes sanitized request details to stderr, followed by a request summary: Hertwill and WooCommerce attempts and failures for the command, and the configured limits (Hertwill: 60 requests/minute per IP on public endpoints, 300 per API key on authenticated ones). Failures include non-2xx responses, network errors, timeouts and JSON parse errors. Hertwill `RateLimit` and `RateLimit-Policy` headers are shown when present. Secrets and authorization headers are never printed in full.

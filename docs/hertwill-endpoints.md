# Hertwill Endpoints

This file tracks endpoint assumptions for Hertwill Doctor v1. Unverified endpoints should not be treated as stable API documentation.

## Lightweight Health Check

- name: lightweight health/check
- HTTP method: unknown
- path: unknown
- auth required: unknown
- request schema, if known: unknown
- response schema, if known: unknown
- verification status: placeholder
- notes: no verified lightweight Hertwill health/check endpoint is available yet. `hwd doctor` must skip Hertwill live connectivity checks instead of calling unverified endpoints or faking success.

## Product Search

- name: product search
- HTTP method: `GET`
- path: `/v1/products/search`
- auth required: yes
- request schema, if known: query parameter `q=<text>`
- response schema, if known: array of product-like objects or wrapped result array; fields may include `id`, `name`, `title`, `sku`, `product_code`, `code`, `price`, `stock`, `availability`, `status`, `images`, `image_urls`
- verification status: verified from public documentation
- notes: `hwd hertwill search` and automatic `hwd diagnose --sku <SKU>` use this endpoint. No Hertwill limit/page parameter is verified in v1, so `--limit` is applied locally after the response. Use `hwd hertwill search --raw` to inspect the exact response body when parsed products are empty.

## Product List

- name: product list
- HTTP method: `GET`
- path: `/v1/products`
- auth required: yes
- query params, if known: pagination/limit parameters are not verified
- response schema, if known: array of product-like objects or wrapped result array under `data`, `items`, `products`, or `results`; fields may include `id`, `name`, `title`, `sku`, `product_code`, `code`, `price`, `stock`, `availability`, `status`, `images`, `image_urls`
- verification status: listed in public documentation
- notes: `hwd hertwill list` uses this endpoint read-only. `--limit` and `--contains` are applied locally over the API response. This may not search the full catalog if the endpoint is paginated. Use `hwd hertwill list --raw` to inspect the exact response body and pagination metadata.

## Product By ID

- name: product by ID
- HTTP method: `GET`
- path: `/v1/products/{id}`
- auth required: yes
- request schema, if known: path parameter `id`
- response schema, if known: product-like object; fields may include `id`, `name`, `title`, `sku`, `price`, `stock`, `availability`, `status`, `images`, `image_urls`
- verification status: needs verification
- notes: used by `hwd hertwill product --id <ID>`, `hwd compare`, and `hwd woo repair-images` (to read Hertwill image URLs; this endpoint is never written to).

## Sync Status

- name: product sync status
- HTTP method: `GET`
- path: `/v1/products/{id}/sync-status`
- auth required: yes
- request schema, if known: path parameter `id`
- response schema, if known: arbitrary JSON object
- verification status: unverified (returns 404 against the live API; `hertwill.SyncStatusEndpointVerified()` returns `false`)
- notes: `hwd diagnose` does not call this endpoint automatically while it is unverified, even when `--hertwill-id` is provided — calling it produced noisy, unactionable 404 warnings on every run. `hwd hertwill sync-status --id <ID>` remains available but does not call the endpoint either; it exits with a non-zero status and prints "Hertwill sync-status endpoint is not verified yet." instead of pretending success. Once this endpoint is verified (flip `SyncStatusEndpointVerified` to `true` in `internal/hertwill/endpoints.go` and update this entry), both call sites will use it normally.

## Login

- name: email/password login
- HTTP method: `POST`
- path: `/v1/auth/login`
- auth required: no
- request schema, if known: `{ "email": "user@example.com", "password": "secret" }`
- response schema, if known: may include `access_token` or `token`
- verification status: needs verification
- notes: Hertwill Doctor does not attempt email/password login in v1 while this endpoint remains unverified. Provide `HERTWILL_ACCESS_TOKEN` instead.

## Register

- name: register
- HTTP method: `POST`
- path: `/v1/auth/register`
- auth required: no
- request schema, if known: unknown
- response schema, if known: unknown
- verification status: documented, not implemented
- notes: not used by Hertwill Doctor v1.

## API Keys

- name: API keys
- HTTP method: `POST`
- path: `/v1/api-keys`
- auth required: yes
- request schema, if known: unknown
- response schema, if known: unknown
- verification status: documented, not implemented
- notes: write/action endpoint, not used by read-only v1.

## Import List

- name: list import list
- HTTP method: `GET`
- path: `/v1/import-list`
- auth required: yes (API key as Bearer token)
- request schema, if known: `page`, `per_page` (20 used), optional `status`
- response schema, if known: `{"data": [{"id", "product_id", "name", "sku", "status", "price", "currency", "default_store_markup", "variations": [{"id", "dropship_id", "sku", "gtin"}]}], "meta": {"pagination": {"page", "per_page", "total", "page_count"}}}`
- verification status: verified against the live API (2026-10)
- notes: `id` is the catalog product ID, `product_id` the store dropship ID, `price` the wholesale cost. Without `status`, synced products are omitted. Some products report `already_exists` on import but never appear here; syncing those returned 403.

## Add To Import List

- name: add products to import list
- HTTP method: `POST`
- path: `/v1/import-list/products`
- auth required: yes
- request schema, if known: `{"product_ids": [8096, 8097]}` (1 to 50)
- response schema, if known: HTTP 201, `{"data": [{"product_id", "status": "added|already_exists|not_found|error", "variations": [{"id", "dropship_id", "sku"}]}]}`
- verification status: verified against the live API (2026-10)
- notes: used by `hwd hertwill import`.

## Sync Products

- name: sync products
- HTTP method: `POST`
- path: `/v1/sync/products`
- auth required: yes
- request schema, if known: `{"product_id", "default_store_markup", "currency", "variations": [{"id", "dropship_id", "default_store_markup"}]}`
- response schema, if known: HTTP 202, `{"data": {"product_id", "status": "syncing", "message"}}`
- verification status: verified against the live API (2026-10)
- notes: `default_store_markup` is the absolute selling price, despite the name and docs. A multiplier like `2.0` is rejected with 422 `PRICE_BELOW_COST`. Products with variants need every variation or the API returns 422 "Variations are required for this product". 403 `FORBIDDEN` "You are not allowed to sync this product" was seen for products hidden from the import list. Used by `hwd hertwill sync`.

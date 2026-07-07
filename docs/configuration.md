# Configuration

Hertwill Doctor reads configuration from command flags, environment variables, a local `.env` file, and built-in defaults.

Configuration precedence:

1. command flags, where available
2. environment variables
3. `.env` file
4. built-in defaults

Environment variables override `.env` values.

## Variables

### WooCommerce

WooCommerce configuration is valid only when all three values are present:

- `WOO_BASE_URL`
- `WOO_CONSUMER_KEY`
- `WOO_CONSUMER_SECRET`

If only part of the credential set is configured, `hwd doctor` reports an incomplete configuration warning/error.

### Hertwill

Hertwill configuration is valid when either:

- `HERTWILL_ACCESS_TOKEN` is present

or:

- `HERTWILL_EMAIL` and `HERTWILL_PASSWORD` are both present

For v1, static access token authentication is the clean supported path. Email/password authentication returns a clear error until the login/token endpoint is verified.

Optional:

- `HERTWILL_BASE_URL`

Default:

- `https://api.hertwill.com`

### Timeout

`HWD_TIMEOUT_SECONDS` controls the timeout for all Hertwill and WooCommerce HTTP requests.

Rules:

- must be a positive integer
- defaults to `30`
- invalid values fall back to `30` and are reported by `hwd doctor`

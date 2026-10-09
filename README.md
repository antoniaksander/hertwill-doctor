# Hertwill Doctor

**Manage your Hertwill dropshipping products in WooCommerce from the command line, or let an AI agent do it for you.**

If you sell [Hertwill](https://hertwill.com) products in a WooCommerce shop, `hwd` lets you:

- see your Hertwill import list, with costs and variants
- add products to the import list and sync them to your shop at the price you choose
- set prices, categories, brand and publish status in WooCommerce
- find out why a product looks wrong (missing images, wrong price, failed sync)

It talks to the Hertwill and WooCommerce APIs directly. Every change needs an explicit `--dry-run` or `--confirm`, prices are always the final selling price, and synced products stay private until you publish them.

## Quick start

You need Go 1.22+, a Hertwill API key, and a WooCommerce REST API key with Read/Write access.

```sh
git clone https://github.com/antoniaksander/hertwill-doctor.git
cd hertwill-doctor
go build -o hwd ./cmd/hwd      # Windows: go build -o hwd.exe ./cmd/hwd
cp .env.example .env           # fill in your store name and keys
./hwd doctor
./hwd hertwill import-list
```

Sync a product at your price, check it, then publish:

```sh
./hwd hertwill sync --id 9107 --price 40.95 --dry-run
./hwd hertwill sync --id 9107 --price 40.95 --confirm
./hwd woo product --sku <sku>
./hwd woo set-status --id <woo id> --status publish --confirm
```

Reprice a product that is already live (simple or variable):

```sh
./hwd woo set-price --id <woo id> --price 94.95 --dry-run
```

`./hwd help` lists every command. Several stores and Windows: [docs/setup.md](docs/setup.md).

## Use it with an AI agent

Point the agent at this repo:

```text
Use https://github.com/antoniaksander/hertwill-doctor to manage my Hertwill products in WooCommerce.
My local copy is in $HWD_DIR. Read its AGENTS.md first, run hwd doctor, then wait for my task.
```

The agent follows [AGENTS.md](AGENTS.md): it checks which store it is connected to and asks before every change. See [docs/starting-prompt.md](docs/starting-prompt.md) for a longer prompt.

## Limits

- Re-syncing a product that is already live can fail on Hertwill's side and leaves it private.
- Products Hertwill hasn't approved for your store return 403 and must be unblocked in Hertwill.
- Products only: no order handling.

## Documentation

- [Setup](docs/setup.md): new machine, new store, AI agents
- [Commands](docs/commands.md): every command and flag
- [Configuration](docs/configuration.md): environment variables and `.env`
- [Hertwill endpoints](docs/hertwill-endpoints.md): what is verified in the API
- [Agent guide](AGENTS.md)

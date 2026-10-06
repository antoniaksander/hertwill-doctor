# Setup

How to set up `hwd` on a new machine, for a new store, and for Claude Code or Codex.

## 1. What you need

- Git and Go 1.22+ (Windows: `winget install GoLang.Go`, macOS: `brew install go`)
- A **Hertwill API key** (`hw_live_...`) from the store's Hertwill account
- A **WooCommerce REST API key** with **Read/Write** access: WooCommerce > Settings > Advanced > REST API
- In Hertwill, the store's WooCommerce integration connected (hertwill.com/account/integration)

## 2. Install

```sh
git clone https://github.com/antoniaksander/hertwill-doctor.git
cd hertwill-doctor
go build -o hwd ./cmd/hwd      # Windows: go build -o hwd.exe ./cmd/hwd
go test ./...
```

Keep the folder **outside** any website folder, so the keys can never be served by the site.

## 3. Configure a store

Copy `.env.example` to `.env` and fill it in yourself (an AI agent should never see these values):

```sh
HWD_STORE_NAME=Roxder
WOO_BASE_URL=https://example.com
WOO_CONSUMER_KEY=ck_...
WOO_CONSUMER_SECRET=cs_...
HERTWILL_ACCESS_TOKEN=hw_live_...   # HERTWILL_API_KEY also works
```

Then check it:

```sh
./hwd doctor
./hwd hertwill import-list
```

`hwd doctor` prints `Store:` and `Config file:` so it's always clear which shop you're working on.

### More than one store

Keep one config file per store, for example in a `stores/` folder next to the repo (not committed):

```text
stores/roxder.env
stores/client-b.env
```

Point `hwd` at one with `HWD_ENV_FILE`:

```sh
HWD_ENV_FILE=../stores/client-b.env ./hwd doctor          # macOS/Linux
$env:HWD_ENV_FILE="..\stores\client-b.env"; .\hwd.exe doctor   # PowerShell
```

Without `HWD_ENV_FILE`, `hwd` reads `.env` in the current folder.

## 4. Connect your AI agent

Set `HWD_DIR` to the hertwill-doctor folder, so agents can find `hwd` from any project:

```sh
echo 'export HWD_DIR="$HOME/path/to/hertwill-doctor"' >> ~/.zshrc   # macOS
setx HWD_DIR "C:\path\to\hertwill-doctor"                           # Windows, then restart the terminal
```

### Claude Code

Install the skill once, for all projects:

```sh
mkdir -p ~/.claude/skills && cp -R .claude/skills/hertwill-sync ~/.claude/skills/
```

Restart Claude Code; `/skills` should list `hertwill-sync`. Inside this repo, Claude also reads `CLAUDE.md` (which points to `AGENTS.md`).

### Codex

Codex reads `AGENTS.md` automatically inside this repo. From another project (for example the shop's theme), start the chat with the prompt in `docs/starting-prompt.md`, which tells it to read `AGENTS.md` and the workflow first.

## 5. Start working

Use `docs/starting-prompt.md` for the first message of a new chat, then ask for what you need, for example:

> Import the Breden scarves from my import list. Check competitor prices, propose prices with profit after VAT, keep them private until I approve.

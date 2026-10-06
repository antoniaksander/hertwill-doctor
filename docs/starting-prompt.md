# Starting prompt

Shortest version, enough for most chats:

```text
Use https://github.com/antoniaksander/hertwill-doctor to manage my Hertwill products in WooCommerce.
My local copy is in $HWD_DIR. Read its AGENTS.md first, run hwd doctor, then wait for my task.
```

Longer version with every rule spelled out. Paste this as the first message of a new Claude Code or Codex chat, from any project. Replace the store name if you have several.

```text
I manage a WooCommerce shop whose products come from Hertwill. Use Hertwill Doctor (hwd) for anything Hertwill or WooCommerce.

1. Read $HWD_DIR/AGENTS.md and $HWD_DIR/.claude/skills/hertwill-sync/SKILL.md (on Windows: $env:HWD_DIR). Follow them.
2. Run hwd doctor from $HWD_DIR and tell me which store it's connected to. The store for this chat: <STORE NAME>.
   If I have several stores, use HWD_ENV_FILE=<path to that store's .env>.
3. Never read or print .env files or keys.
4. Dry runs and read commands are fine. Ask me before every --confirm.
5. Keep reports short: one table, then what you need from me.

Then wait for my task.
```

Example tasks after that:

- "Show what's in my import list for brand X, with cost and number of variants."
- "Check competitor prices for these products and propose prices that stay profitable with a 10% discount code. Prices include 24% VAT."
- "Sync these at the approved prices, set categories like product <ID>, keep them private and show me the table."
- "Publish the products from the last table."

---
name: update-translations
description: >
  Translate outstanding app strings, the user manual, or both. A script lists
  what changed. Grok translates that list in large batches. Default is the app,
  changed sentences only. Use for /update-translations.
when-to-use: "update translations, translate new strings, translate the manual, /update-translations"
user-invocable: true
---

# update-translations

Coder session. No plan. If this checkout has no app and no user manual, stop and say so.

Read this checkout's `docs/facts/translations.md` and follow it.

Run `./scripts/i18n-outstanding`. Add `--manuals` only when the user asked for manuals. Add `--all` only when the user asked to regenerate everything. Overrides stay off the list. Developer docs stay off the list.

Translate that list in this session. You are Grok. No other translation service. A few sentences is one batch. A regenerate-all list is still large batches, never one sentence at a time. Send each sentence whole, format tokens included.

Write the catalog `text` and `source` for those entries only. Do not change English in code, JSON, or `docs/user-manual/manual.md`. Do not edit an override file. Do not edit screenshots.

Run the script's check on what you wrote. A reply whose format tokens differ is not kept. Fix that batch and check again.

Commit with this checkout's `project-facts.md` Build and Commit lines.

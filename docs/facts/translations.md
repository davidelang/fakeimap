# Translations

A person edits English only in the file that already defines the feature. Scripts and `/update-translations` do the rest. `strings.xml`, locale packs, and the machine catalog are not places to change wording.

## Definition sites

- Code: `S("Save")`, `S("Version %1$s", version)`. The English sentence is the argument. A plural uses the helper next to `S`.
- A JSON file, when that feature already stores its English sentence there.
- The user manual, when the app has one: `docs/user-manual/manual.md`.

Resource names are generated. Call sites do not use `R.string`.

Developer docs stay English. Short in-app help sentences are app strings, not the manual.

## Catalog

`app/src/main/i18n/<locale>.json` holds `key`, `source` (the English sentence at last translation), and `text`. Generated `values-<locale>/strings.xml` is build output and is not committed.

`app/src/main/i18n/keep-terms.txt` is one term per line: proper names, people, projects, protocol names. A term that is the entire sentence is copied, not translated.

`app/src/main/i18n/<locale>.override.json` is a human translation. The build uses it. The skill does not write it. When the English changes and the override's format tokens no longer match, the build shows English for that entry and `./scripts/i18n-outstanding` reports it. The model still does not translate it.

English regional locales are machine-translated like every other locale.

Right-to-left locales stay unshipped.

## Outstanding script

`./scripts/i18n-outstanding` decides the work. The model does not. It scans the definition sites and compares them to the catalog. It prints added, changed, and removed sentences. Unchanged sentences are not shown to the model.

No flags: app strings that changed. `--manuals` adds the user manual. `--all` lists every machine sentence. Both flags may be set together.

Before a sentence is sent, the script removes format tokens and keep-terms. If nothing remains, it is not sent. `Version %1$s` is sent, because "Version" remains. A string that is only `%1$s`, or only a keep-term, is not sent.

The whole sentence is what the model sees, format tokens included. The model copies every format token unchanged. The script rejects a reply whose token set differs, leaves the previous text, and marks the entry pending. The build then shows English for that entry. Word order may move a token. `Versión %1$s` and `%1$s sürümü` are both valid.

Overrides are skipped in every mode. Developer docs are never in scope.

## Build

A normal build packages every shipped locale from the catalog. It does not call a translator and it does not use the network. It does not edit the catalog or the English definition.

A new or changed sentence with no matching translation is packaged as the English. The build succeeds and prints how many sentences fell back. There is no English-only build flag.

## Skill

`/update-translations` runs in a coder session. No plan. The coder runs `./scripts/i18n-outstanding`, translates only that list, in large batches, and commits with this checkout's Build and Commit lines. Grok writes the text. No other translation service.

The usual case is a few new or changed sentences, so one batch covers them. `--all` is still batched. The model does not scan the catalog to find changes.

The skill does not change English wording in code, JSON, or the manual.

## Manuals

These rules apply when the app has a user manual. They are not a reason to add one.

The APK bundles the English manual only.

The manual reader has two actions, and only there. Help and About open the full manual. They do not get a translate action.

- **This language on the web** opens the published translation for the app's current language. Hidden when that page does not exist. Hidden when the app is in English.
- **Translate the latest English** opens the public English HTML through Google Translate. The app does not upload the manual. Hidden until that public URL exists.

Published translations are static HTML on GitHub so each language can show its own screenshots. Until a language has been photographed, its page points at the English image. The text skill does not invent screenshots and does not copy the English file into each language directory.

Stored screenshots are JPEG, maximum width 1080, quality about 75, metadata stripped. They are normal git files, not Git LFS. Full-resolution captures are not committed.

Screenshot capture is a separate tool. It is not part of the translation build or `/update-translations`.

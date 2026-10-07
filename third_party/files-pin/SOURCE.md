# files-pin example

Demonstrates non-git `[[source]]` materialize with seed + sha256.

Place this directory next to the libpin kit tools as `third_party/files-pin/` (a copy already lives under `kit/files-pin/` for demos).

```bash
cd kit   # or your-repo/third_party
./fetch-deps ro files-pin
cat files-pin/src/hello.txt
```

| Field | Value |
|-------|--------|
| `source_kind` | `files` |
| `build` | `[]` (materialize only) |
| `[[source]]` | `seed` + `path` + required `sha256` |

See [docs/PIN_FORMAT.md](../../docs/PIN_FORMAT.md) for hash policy and URL rules (HTTPS / file / seed only — never SSH).

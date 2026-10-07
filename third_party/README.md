# libpin kit — drop-in `third_party/` tooling

Copy or rsync this directory to your project’s `third_party/` (or use it in place for demos).

## Tools

| File | Role |
|------|------|
| `fetch-deps` | Materialize pins (git / `[[source]]`), RO/RW, build orchestration |
| `get-artifacts` | Collect `[[artifact]]` rows from `libpin.toml` |
| `libpin-sandbox` | bwrap outer + Landlock inner |
| `libpin-bwrap` | bubblewrap helper |
| `libpin-landlock` | Landlock mutation confinement (Python) |

## Bundled pins

| Dir | Purpose |
|-----|---------|
| `example/` | Toy git-style pin (seed-src, no network) |
| `files-pin/` | Minimal non-git `[[source]]` pin |

## Quick demo

```bash
cd "$(dirname "$0")"   # this kit directory
./fetch-deps ro example files-pin
./fetch-deps build example
cat example/artifact/hello.bin
cat files-pin/src/hello.txt
./libpin-landlock --status
```

## Docs

See the parent repo:

- [../docs/QUICKSTART.md](../docs/QUICKSTART.md)
- [../docs/PIN_FORMAT.md](../docs/PIN_FORMAT.md)
- [../docs/SANDBOX.md](../docs/SANDBOX.md)
- [../docs/GUIDE.md](../docs/GUIDE.md)
- [../docs/CASE_STUDY_VEHICLEEXPENSES.md](../docs/CASE_STUDY_VEHICLEEXPENSES.md)

## Requirements

bash, python3 (`tomllib` or `tomli`), git (for git pins). Optional: `bubblewrap`, Landlock-capable kernel.

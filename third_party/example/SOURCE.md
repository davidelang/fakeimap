# example — toy third_party pin

Demonstrates the pin contract without network or heavy toolchains.

```bash
# From the kit directory (acts as third_party/)
./fetch-deps ro example    # creates a tiny git tree in src/ + optional patch
./fetch-deps build example # build + get-artifacts
# or:  ./example/build && ./get-artifacts example
cat example/artifact/hello.bin
```

| Field | Demo value |
|-------|------------|
| `source_kind` | `seed-src` (declared; empty git_https does not imply it) |
| `default` | `false` (omitted from the default NAME set) |
| `build_time` | `minutes` |
| `reproducible` | `false` (timestamp in output **filename**; content is stable) |
| `from` glob + `pick = "newest"` | `src/bin/hello-*.bin` |
| `sha256` | required; content is `hello from third_party/example pin 0.0.1\n` |

Full rules: [docs/PIN_FORMAT.md](../../docs/PIN_FORMAT.md), [docs/GUIDE.md](../../docs/GUIDE.md).

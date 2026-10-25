# agent-coordination-tickets

Channel programs for passing one file from one agent to the next.

| | |
|--|--|
| **Upstream** | `davidelang/agent-coordination-tickets` @ `89df8eda034af394ecc99d9cc1594b3b2b8ba851` (`master`) |
| **Profile** | Source plus one build step: `go build` of `src/go/channel` (static, stdlib only, reproducible); the result is committed as `artifact/channel` (sha256 in libpin.toml). |
| **Host clone** | `/home/dlang/git/agent-coordination-tickets` |

Agents run only the committed `artifact/channel`, through its links `artifact/channel-setup`, `-arm`, `-send`,
`-ping` and `-status`, from the repo root; they never need `src/`. To join a channel an agent runs
`artifact/channel-arm <diag> <node> --whatineedtoknow` (the `/channel` skill) and follows its output.
`src/bin/channel` (POSIX sh) and `src/bin/channel.py` are alternative implementations of the same tool, kept in
step by the connector's tests; agents do not run them. Consumers do not rerun the connector's tests when bumping this pin.

Bumping the pin (maintainers): `./third_party/fetch-deps rw|ro agent-coordination-tickets` gives a `src/` checkout;
build with `./third_party/fetch-deps build agent-coordination-tickets`, then commit `artifact/channel`, `git_sha`
and the artifact `sha256` together. `fetch-deps build --record agent-coordination-tickets` writes the new
artifact `sha256` into libpin.toml (old -> new printed); `fetch-deps upgrade NAME --ref REF` does the same
together with `git_sha`/`git_describe`.

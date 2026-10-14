# agent-coordination-tickets

Channel programs for passing one file from one agent to the next.

| | |
|--|--|
| **Upstream** | `davidelang/agent-coordination-tickets` @ `acbd9bc127c2dad4f2e5bc733db40e5a28e4b70e` (`master`) |
| **Profile** | Source plus one build step: `go build` of `src/go/channel` (static, stdlib only). |
| **Host clone** | `/home/dlang/git/agent-coordination-tickets` |

```bash
./third_party/fetch-deps ro agent-coordination-tickets
./third_party/fetch-deps build agent-coordination-tickets
```

When that host clone contains the pin SHA, `src/` is a linked worktree of it.
Agents run the Go binary `src/go/channel` (built by the step above); `src/bin/channel` (shell) is the fallback. The names
`channel-setup`, `channel-arm`, and `channel-send` are the same program.

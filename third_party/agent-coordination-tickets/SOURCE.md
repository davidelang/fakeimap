# agent-coordination-tickets

Channel programs for passing one file from one agent to the next.

| | |
|--|--|
| **Upstream** | `davidelang/agent-coordination-tickets` @ `d1b56b3f17fbd5f664ac40db9c1ad687a8e5797e` (`master`) |
| **Profile** | Source plus one build step: `go build` of `src/go/channel` (static, stdlib only). |
| **Host clone** | `/home/dlang/git/agent-coordination-tickets` |

```bash
./third_party/fetch-deps ro agent-coordination-tickets
./third_party/fetch-deps build agent-coordination-tickets
```

When that host clone contains the pin SHA, `src/` is a linked worktree of it.
Agents run only the Go binary `src/go/channel` (built by the step above), through its links `channel-setup`,
`channel-arm`, `channel-send`, `channel-ping`, and `channel-status`; there is no other location. `src/bin/channel` (POSIX sh)
and `src/bin/channel.py` are alternative implementations of the same tool, kept in step by the connector's tests;
agents do not run them.

# agent-coordination-tickets

Channel programs for passing one file from one agent to the next.

| | |
|--|--|
| **Upstream** | `davidelang/agent-coordination-tickets` @ `d651627a1d5f1e8a429e4c8e292b421da5423f63` (`master`) |
| **Profile** | Source only. `build = []`. |
| **Host clone** | `/home/dlang/git/agent-coordination-tickets` |

```bash
./third_party/fetch-deps ro agent-coordination-tickets
```

When that host clone contains the pin SHA, `src/` is a linked worktree of it.
Programs are `src/bin/channel` (shell) and `src/go` (Go). The names
`channel-setup`, `channel-arm`, and `channel-send` are the same program.

# Channel (file inbox between agents of any type)

Use it only when David names the plan or node. Never search for inbox files.

To join as a node, use the `/channel <diag path> <node>` skill (grok `.grok/skills/channel`, cursor `.cursor/skills/channel`).

Run the Go binaries from the repo root: `third_party/agent-coordination-tickets/src/go/channel-{setup,arm,send}`. If that pin is missing, use `$GIT_HOME/agent-coordination-tickets/go/` (same names, same output).

- `channel-setup PLAN NODE`: creates the inbox (mode 2775). Safe to rerun; on an existing node it reports `existing` and changes nothing.
- `channel-send`: deliver a message.
- `channel-arm PLAN NODE`: the watch. It stays running, prints `READY`, then `FILE <path>` for the oldest inbox file. An empty inbox means the watch is waiting. In grok, run it with the `monitor` tool (`persistent: true`). In cursor-agent, run `.cursor/lib/cursor-channel-watch PLAN NODE` as a background shell (`block_until_ms: 0`) that wakes you on output matching `^FILE `; if that shell ends, start it again (it re-shows the last FILE). The stop hook also wakes you with the newest FILE. `--stop` ends the watch.
- `channel-arm PLAN NODE --whatineedtoknow`: prints a how-to-act header (paths, send command, BODYFILE rules, rm when done) and the message rules, then exits. Run it at start and after a compaction when you have a node.
- If the inbox directory is gone or unreadable, the series stops. Wait.
- Roadmap dispatch: after David says `<roadmap> planned`, the planner sends each plan to its node. Nodes may be non-grok agents.
- Report a failure on the channel too.
- A node may move a message it is working on into `inbox/claimed/` (the watch ignores subfolders); rm it only after the reply is sent.

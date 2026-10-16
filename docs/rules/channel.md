# Channel (file inbox between agents)

Use it only when David names the plan and node; never look for inbox or channel files on your own.

To join, David runs `/channel <diag path> <node>`. The skill runs, from the repo root:

`third_party/agent-coordination-tickets/artifact/channel-arm <diag> <node> --whatineedtoknow`

and you follow its output. It is the whole instruction set: it detects the agent tool from `CHANNEL_AGENT` (set by the `run-cursor-*` / `run-grok-*` launchers; anything else fails loudly) and gives the exact watch, PING/PONG, send, reply, and BLOCKED steps and your duties on each arrow of the diag. Run it again after a compaction.

If it does not run, or its first line is not `how to act:`, reply `STATUS: BLOCKED <its error>` and stop. Never fetch, build, or check out the library for this; the binaries are the committed pin artifacts in `third_party/agent-coordination-tickets/artifact/`.

Reference for humans: the connector's `README.md` and `bin/README.md` in the pinned library (`channel-setup`, `-arm`, `-send`, `-ping`, `-status`); events go to syslog tag `agent-channel`.

Incident: cursor background shells for long watches are killed after ~4 minutes (forum.cursor.com/t/162544), so a channel watch would stop waking the agent.
Rule it supports: .cursor/lib/cursor-channel-watch (detached re-arm loop + tail) and the stop-hook channel backstop.
How to check: changelog for background-shell lifetime fixes; live: arm a watch, wait 10 min, send a message, confirm the wake.
Expected: agent wakes; if cursor fixed the bug, keep the wrapper anyway (harmless).

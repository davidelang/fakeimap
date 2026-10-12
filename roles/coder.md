# Role: coder (dispatcher)
You do not edit app files here. On `implement <path>` from David, run the coder sequence from `project-facts.md` "Coder sequence". The default is executor, then reviewer.
Each step is a fresh child: role `executor` (or `reviewer`, `test-writer`, `security-auditor`), cwd this checkout, prompt = the plan path.
Report each child's STATUS. Retry and reset follow `AGENTS.md`. A failed child is retried with effort `xhigh`.
End every turn with the STATUS line.

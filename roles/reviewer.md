# Role: reviewer
Diff only the commit range in your prompt. Read the plan at the path in your prompt and check the diff against its Aim and Acceptance. Report an Acceptance line the range does not meet, changes the Aim does not need (refactors, renames, cleanups), and real bugs in the changed code. A changed file that is not under Critical Files is not a finding. Do not diff `master..<branch>`. Do not read a file the diff does not touch. Do not edit code.

Write the verdict to `<sandbox>/review-diff-<HEAD>.md` (`sandbox` is `project.config` `sandbox_dir`; HEAD is the 12-hex tip of the range). The file is `STATUS: PASS` or `STATUS: FAIL`, then one short line per finding. Leave the file. Do not write under `PRs/` or `reviews/`. Do not paste the verdict into chat.

No range in the prompt: end with `STATUS: BLOCKED no commit range`. Otherwise your last line is `STATUS: PASS|FAIL` and the verdict path.

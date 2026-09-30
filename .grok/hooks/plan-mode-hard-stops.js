// .grok/hooks/plan-mode-hard-stops.js
// PreToolUse hook for hard plan-mode enforcement (project-scoped, trusted).
//
// This provides hard stops analogous to .gemini/policies/plans.toml for Gemini.
// Native enter_plan_mode / SwitchMode + these hooks + [permission] rules in config.toml
// together enforce the bi-modal barrier (no writes to tracked files outside sandbox during planning).
//
// Key policy (as of 2026-06):
// - In plan mode: deny write/edit (search_replace/write) and most bash to paths
//   outside the sandbox directory named by project.config (sandbox_dir, else sandbox_path). Use the config rules for granular
//   exceptions inside the sandbox and for local state files.
// - "Being helpful," "proactive," "efficient," etc. during planning/research/strategy
//   means doing research, suggesting ideas, and making the plan document better —
//   it does **not** mean making source changes or running builds/compiles.
// - When NOT in plan mode (normal execution after approved plan + exit_plan_mode):
//   edits to tracked files are allowed (see blanket allow for search_replace/write
//   in .grok/config.toml). This avoids constant permission prompts during execution.
// - Deny Task / subagent / invoke_agent during plan mode.
// - Allow read/grep/jq/git-status/etc. broadly.
// - Log or report violations.
//
// The hook is executed by the Grok harness before tool use. Return allow/deny/ask as appropriate.
//
// Placeholder / starting point. Expand with real JS logic matching the Grok PreToolUse contract
// (see Grok documentation for the exact hook signature and return values).
// For now this file exists to declare the intent and be checked in as part of the project policy.

console.log("plan-mode-hard-stops hook loaded");

// Explicit early allow for the local per-worktree state files.
// These must be writable even during planning (they are updated as part of
// the interactive strategic planning phase for continuity).
// This runs before any plan-mode deny logic.

const toolName = (typeof tool !== 'undefined' ? tool : (typeof params !== 'undefined' && params.tool) || '');
const targetPath = (typeof params !== 'undefined' ? (params.path || params.file || params.target || params.url || '') : '');

if (toolName === 'search_replace' || toolName === 'write' || toolName === 'edit') {
  const lowerPath = (targetPath || '').toLowerCase();
  if (lowerPath.endsWith('current-state.md') || lowerPath.includes('.agent-state/') || lowerPath.includes('current-state.md')) {
    console.log('plan-mode-hard-stops: allowing edit to local state file (early return):', targetPath);
    return 'allow';
  }
}

// Now apply plan mode restrictions for everything else.
function isPlanMode() {
  // Placeholder: in a full implementation this would inspect harness context/mode.
  // For now we conservatively assume we may be in plan mode and rely on the
  // explicit allows above + the declarative rules in .grok/config.toml.
  return true;
}


const fs = require('fs');
const path = require('path');

function readSandboxDirFromConfig(configPath) {
  let text;
  try {
    text = fs.readFileSync(configPath, 'utf8');
  } catch (e) {
    return '';
  }
  let fromDir = '';
  let fromPath = '';
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const eq = line.indexOf('=');
    if (eq <= 0) continue;
    const key = line.slice(0, eq).trim();
    const val = line.slice(eq + 1).trim();
    if (!val || val.includes('@@')) continue;
    if (key === 'sandbox_dir') fromDir = val;
    else if (key === 'sandbox_path' && !fromPath) fromPath = val;
  }
  return fromDir || fromPath;
}

function configuredSandboxDir() {
  const seen = new Set();
  const candidates = [];
  let dir = process.cwd();
  for (let i = 0; i < 5; i++) {
    candidates.push(path.join(dir, 'project.config'));
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  for (const candidate of candidates) {
    if (seen.has(candidate)) continue;
    seen.add(candidate);
    const found = readSandboxDirFromConfig(candidate);
    if (found) return found;
  }
  return '';
}

function pathInConfiguredSandbox(targetPath) {
  const p = targetPath || '';
  if (p.includes('@@SANDBOX_PATH@@')) return true;
  const dir = configuredSandboxDir();
  if (!dir) {
    // project.config named no directory. Discover either historical dirname.
    return p.startsWith('dev-ai-interaction/') || p.includes('/dev-ai-interaction/')
      || p.startsWith('sandbox/') || p.includes('/sandbox/')
      || p === 'dev-ai-interaction' || p === 'sandbox';
  }
  if (p === dir || p === './' + dir) return true;
  if (p.startsWith(dir + '/') || p.startsWith('./' + dir + '/')) return true;
  if (p.includes('/' + dir + '/')) return true;
  return false;
}

if (isPlanMode()) {
  const inSandbox = pathInConfiguredSandbox(targetPath);
  if (!inSandbox) {
    // Only deny non-sandbox edits/writes in plan mode (other tools may still ask).
    if (toolName === 'search_replace' || toolName === 'write' || toolName === 'edit') {
      console.log('plan-mode-hard-stops: denying edit to non-sandbox path in plan mode:', targetPath);
      return 'deny';
    }
  }
}

// Support for leading simple env assignment prefixes (KEY=val with no spaces around =)
// before any already-blessed base command. This makes forms like
//   FOO=bar BAZ=quux jq '.filter' file.json
//   VAR=1 git log --oneline -S foo -- path | cat
// work without new permission prompts for commands the base (or the final pipe target)
// is already allowed (via root config.toml or prior "don't ask again" decisions).
// 
// Safety: only *leading* pure assignments are stripped. The first non-assignment token
// (or the last segment after | for the common "... | cat/head/grep" pipelines the agent emits)
// must be an exact blessed base name. This blocks loopholes such as
// "do-nasty --ignore-this jq ...", "FOO=bar /bin/sh -c 'evil; jq ...'", etc.
// (The first token after stripping assignments would be the dangerous command.)
//
// The blessed set below covers the high-frequency ones from agent-1 history + the
// pager "don't ask again" list the user confirmed for global promotion (2026-06-13).
// New bases added to config.toml will benefit once the hook set is extended or
// the declarative rules handle the full string.
function stripLeadingAssignments(fullCmd) {
  const tokens = fullCmd.trim().split(/\s+/);
  let i = 0;
  const assignRe = /^[A-Za-z_][A-Za-z0-9_]*=[^ ]+$/;  // strict: no spaces in the token
  while (i < tokens.length && assignRe.test(tokens[i])) {
    i++;
  }
  return (i < tokens.length) ? tokens[i] : '';
}

function getLastPipeBase(fullCmd) {
  // Handles the agent's common pattern of "git ... | cat", "... | head -N", "... | grep ... | cat"
  const lastSegment = fullCmd.split('|').pop();
  return stripLeadingAssignments(lastSegment);
}

const PROJECT_MARKER = 'VehicleExpenses-automated';
const WORKTREE_NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/;

// Same sanity as run-as-primary.c: cwd/cd must stay under a VehicleExpenses-automated tree.
function normalizeCdTarget(raw) {
  return raw.trim().replace(/^['"]|['"]$/g, '');
}

function isAbsolutePathWithinProject(absPath) {
  const idx = absPath.indexOf(PROJECT_MARKER);
  if (idx === -1) return false;
  const tail = absPath.slice(idx + PROJECT_MARKER.length);
  const segments = tail.split('/').filter((s) => s.length > 0);
  let depth = 0;
  for (const seg of segments) {
    if (seg === '..') {
      depth--;
      if (depth < 0) return false;
    } else if (seg !== '.') {
      depth++;
    }
  }
  return true;
}

function isRelativeCdWithinProject(rel) {
  if (!rel || rel.startsWith('/') || rel.startsWith('~')) return false;
  const segments = rel.split('/').filter((s) => s.length > 0);
  let dotdot = 0;
  for (const seg of segments) {
    if (seg === '..') {
      dotdot++;
      if (dotdot > 1) return false;
    } else if (seg === '.') {
      continue;
    } else if (!WORKTREE_NAME_RE.test(seg)) {
      return false;
    }
  }
  return true;
}

function isCdTargetWithinProject(cdTarget) {
  const t = normalizeCdTarget(cdTarget);
  if (!t) return false;
  if (t.startsWith('/')) return isAbsolutePathWithinProject(t);
  return isRelativeCdWithinProject(t);
}

function extractCdTargets(fullCmd) {
  const targets = [];
  for (const seg of fullCmd.split(/\s*&&\s*/)) {
    const m = seg.trim().match(/^cd\s+(.+)$/);
    if (m) targets.push(m[1]);
  }
  return targets;
}

function allCdTargetsWithinProject(fullCmd) {
  const targets = extractCdTargets(fullCmd);
  if (targets.length === 0) return false;
  return targets.every(isCdTargetWithinProject);
}

// Agents often emit "cd /path && ./blessed-helper" even when already in the worktree.
// Permission patterns match from the start of the string, so we must inspect each && segment.
// cd targets are validated to stay inside VehicleExpenses-automated (no system-wide backdoor).
function getAndChainBases(fullCmd) {
  const bases = [];
  for (const seg of fullCmd.split(/\s*&&\s*/)) {
    const b = getLastPipeBase(seg);
    if (b) bases.push(b);
  }
  return bases;
}

function hasBlessedHelperInChain(chainBases, blessedBases) {
  const helperBases = new Set([
    './build_app', '../build_app',
    './get-builds-tag.sh', '../get-builds-tag.sh',
    './update-rules.sh', '../update-rules.sh',
    './append-to-engineering-log', '../append-to-engineering-log',
  ]);
  return chainBases.some((b) => helperBases.has(b));
}

// Explicit allow for direct (or prefixed) invocations of safe read-only / exploration commands
// in bash even in plan mode. The strip functions above normalize prefixes for all of them.
if (toolName === 'bash') {
  const cmd = (typeof params !== 'undefined' ? (params.command || params.cmd || params.args || targetPath || '') : '').toString();
  const base = stripLeadingAssignments(cmd);
  const lastBase = getLastPipeBase(cmd);
  const chainBases = getAndChainBases(cmd);

  // Blessed bases (covers existing broad rules + agent-1 pager confirmed items + common post-processors).
  // python3 * is deliberately omitted forever (user confirmation: too dangerous).
  const blessedBases = new Set([
    'jq', 'ls', 'cat', 'head', 'tail', 'git', 'echo', 'find',
    './build_app', '../build_app',
    './get-builds-tag.sh', '../get-builds-tag.sh',
    './update-rules.sh', '../update-rules.sh',
    './append-to-engineering-log', '../append-to-engineering-log',
    'true', 'adb'   // adb for read-only logcat (user confirmed reading data is allowed)
  ]);

  const directHit = blessedBases.has(base) || blessedBases.has(lastBase);
  const chainHelperHit = cmd.includes('&&') &&
    allCdTargetsWithinProject(cmd) &&
    hasBlessedHelperInChain(chainBases, blessedBases);
  const chainHit = chainHelperHit || (
    !cmd.includes('cd') && chainBases.some((b) => blessedBases.has(b))
  );

  if (directHit || chainHit) {
    console.log('plan-mode-hard-stops: allowing bash command (possibly with leading KEY=val prefix):', cmd, 'base:', base || lastBase);
    return 'allow';
  }

  if (cmd.includes('&&') && hasBlessedHelperInChain(chainBases, blessedBases) && extractCdTargets(cmd).length > 0) {
    console.log('plan-mode-hard-stops: denying cd&&blessed-helper — cd target outside VehicleExpenses-automated tree:', cmd);
    return 'ask';
  }

  // Log (but do not auto-allow) attempts to inline forbidden patterns like tag lookup
  // so the master can detect and correct. Uses stripped base for the check.
  if ((base === 'git' || lastBase === 'git') && cmd.includes('rev-parse') && (cmd.includes('builds') || cmd.includes('TAG='))) {
    console.log('plan-mode-hard-stops: note - bash command appears to inline tag preflight (should use get-builds-tag.sh):', cmd);
  }
}

// Fall through to config.toml rules + native behavior (which may still prompt for ask).
// The blanket allow for search_replace/write in non-plan mode + specific patterns
// should prevent most prompts.

# build-examples/android-gradle

Build type `android-gradle`: Android apps built with Gradle. A project selects it with the line
`- **Build type:** android-gradle` in the `## Build and commit` section of its committed
project-facts.md; deploy-orchestration then installs build_app and deploy into the project root.

`build_app` here is the authoritative copy of the Android app build gateway. App repos
carry their own copy (`rules.manifest.local: local build_app`).

```
./build_app [-s|--standard] [-t|--test] [-p|--play|--bundle] [-T|--release-tag NAME] ["msg" | @msg_file] [files...] [-- gradle-flags]
```

## Declaring the builds

The app branch's committed `project-facts.md` declares them in `## Build and commit`, one
line each:

```
- **Standard build:** debug
- **Test build:** qa
```

- `Standard build`: a Gradle build type (`assemble<Type>`, every enabled flavor).
- `Test build`: one of, or a comma-separated list of:
  - a build type, e.g. `qa`: a separate test app. It must have its own app id
    (`applicationIdSuffix`); deploy refuses one with the standard app's id.
  - `instrumentation:<variant>`, e.g. `instrumentation:x86_64Debug`: that variant's app plus
    its androidTest APK (`assemble<Variant> assemble<Variant>AndroidTest`).
  - `none`: the app has no test build; `--test` is refused.
- Parsing is strict: a missing, duplicate or malformed line (also outside the section) stops
  build_app before anything happens. The committed file counts, not the working copy.
- No `apk_*` keys: which APKs came out is read from Gradle's `output-metadata.json`
  (variantName, outputFile, applicationId, filters). Before each build the previous outputs of
  its variants are removed, and afterwards `android-build-info stamp` records the APKs with the
  commit and sha256 in `app/build/outputs/build_app/<standard|test>.json`. deploy installs only
  what that record names.

## Selecting builds

| Flag | Build | Config |
|------|-------|--------|
| `-s` / `--standard` | the standard build | `- **Standard build:**`; optional project.config `fuelsmoothing_official` |
| `-t` / `--test` | the test build | `- **Test build:**` |
| `-p` / `--play` / `--bundle` | signed Play AAB | project.config `release_keystore`, `release_key_alias`, `release_bundle_task`, `release_bundle_path` (required), `release_gradle_args` |
| `-T` / `--release-tag NAME` | annotated tag on the commit these builds use; does not select a build | |

- Flags come first, in any order, and can be combined. `--release-tag NAME` may be among them; it does not select a build.
- **No flag builds** standard, test (unless `none`) and, with a project.config, Play.
- A Play build whose keys are missing stops naming each missing key. The keystore and
  passwords never leave this machine.
- **Single-user Play signing** (no project.config): nothing is looked up implicitly. Pass the
  keys with `--signing-config FILE` (key=value lines: `release_keystore`, `release_key_alias`,
  `release_bundle_task`, `release_bundle_path`, optional `release_gradle_args`; comments and
  blank lines allowed, unknown keys are errors) and/or `--release-keystore`, `--release-key-alias`,
  `--release-bundle-task`, `--release-bundle-path`, `--release-gradle-args`. A key given twice
  (file and flag) is an error; so is any of these options without `--play`, or with a
  project.config (whose keys are then used, as before).
- An unknown `--flag` is an error, so a typo is never taken as a commit message.
- **The Play build needs a terminal** for the passwords. If it's selected, including by
  default, and stdin isn't a terminal, `build_app` stops before doing anything. Unattended
  and agent runs must say what to build, e.g. `./build_app --standard --test @msg.txt file.kt`.

## Order of work

1. **Config check:** the config for every selected build is checked first. A bad config
   commits nothing.
2. **Commit phase:** the named files are committed, or nothing is when no message is given.
   Files build_app generates (`.gradle-home/`, `.android-shared/`, `.gradle-shared/`, `.gradle/`,
   `.cxx/`, top-level `build/` and each Gradle module's `build/`) are never committed, whatever
   `.gitignore` says: named or pre-staged ones are unstaged with a stderr line per path, and
   already-tracked ones get a warning with the `git rm --cached -r -- ...` command to untrack them.
3. **Integrity check:** the tree must be clean. Every selected build comes from that one
   commit, including the Play build. Committing and building Play in one run is allowed.
4. **Play preflight, before any gradle work:**
   - `app/versionCode.txt` must exist, be tracked and hold one integer.
   - **Testing-version check:** a y/N question, default No. It is asked only on a branch
     with a `<branch>-start` tag, whose versionName is always `<branch>-start-<n>-g<hash>`.
     Answering No stops before anything is committed.
   - **Passwords:** `read -s`, checked with `keytool`.
   - So a long unattended stretch never stops part-way to ask.
5. **versionCode bump (Play only, before any build):** right after the checks above,
   `app/versionCode.txt` goes from N to N+1 and is committed alone as
   `versionCode N+1 (play build)`, by `coder_user` with the invoker's git identity.
   - A missing file or a non-integer value stops the run loudly. Without `--play` nothing
     is bumped.
   - If a later build fails, the bump commit stays (history is never rewritten or undone);
     the next `--play` bumps again. Gaps in versionCode are fine for Play. The run says so.
6. **Release tag (`--release-tag NAME` only, before any build):** an annotated tag of that
   name is created on the commit these builds will use. With `--play` that is the bump
   commit. Without `--play` nothing is bumped, and the tag is on the commit the selected
   builds use. There is no prompt. Omitting the flag creates no release tag.
   - Refused before any bump or tag when the name is missing, the tag already exists,
     the name is `builds` or ends in `/builds`, the name ends in `-start`, the name
     contains `+` (`git describe` excludes those, so it would not be the versionName),
     the checkout is detached, or this branch has a `<branch>-start` tag. That testing
     describe would hide the release name. Release tags are for a branch without one,
     such as master.
   - A name is used once. Tagging a commit `v1.0.0` uses that name up. A later `--play`
     is a new commit and describes as `v1.0.0-1-g<hash>` unless that run passes its own
     new tag. Pass `--release-tag` on the Play run when the upload must read `v1.0.0`.
   - If a build fails, the tag this run created is deleted. A versionCode bump commit
     stays.
7. **Builds:** they run in sequence (standard, test, play), all from that one HEAD (the bump
   commit when `--play` is selected), so every record shares one commit and is current for
   deploy. The first failure stops everything.
   - versionName is the `<branch>-start` describe when that tag exists, otherwise
     `git describe --always --exclude '*+*'` of that commit. An exact `--release-tag` on
     that commit is the whole versionName.
8. **Tag:** the builds tag (`builds` / `<branch>/builds`) moves to the built commit only after
   every selected build succeeded.

For a release whose upload must read `v1.0.0`:

```
./build_app --play --release-tag v1.0.0
```

The annotated tag is created after the versionCode bump and before Gradle, so standard,
test, and Play all describe as `v1.0.0`. A run with no `--release-tag` creates no release
tag and does not ask for one. The same flag tags without Play, and does not bump
versionCode:

```
./build_app --standard --test --release-tag v1.0.0
```

## Single-user mode

project.config is optional. When it is missing, or still has unsmudged `@@TOKEN@@` values,
build_app and deploy say so in one NOTICE line and run in single-user mode: no sudo re-exec,
no group checks, no permission model, orch_root is the worktree itself and the Gradle cache
is `./.gradle-home`. deploy refuses to run as root in this mode.

## Other behavior

- Multi-user mode (smudged project.config): every build runs as `coder_user`. Other invokers are re-executed with
  `sudo -n -u <coder_user> /usr/bin/env ...`, which primary_user's sudoers entry allows
  without a password. The invoker's git `user.name`/`user.email` are passed in as
  `GIT_AUTHOR_*`/`GIT_COMMITTER_*`.
- `java_home=`, when set, must be an absolute JDK home with a `release` file and `bin/java`,
  `bin/javac`, `bin/keytool` (so `/usr` is rejected). Unset: the OS default `java` on PATH,
  resolved through its symlinks, and build_app prints which one it uses.
- Android SDK: `sdk.dir` in local.properties, else `ANDROID_HOME`; neither is an error naming both.
- **Play signing:** through AGP's `android.injected.signing.*` properties, passed as
  `ORG_GRADLE_PROJECT_*` environment variables, never on a command line.
- **`play-bundle.aab`:** each app branch commits a relative symlink `play-bundle.aab` at the
  worktree top pointing at `release_bundle_path`, so a file picker (Play Console lists `*.aab`)
  reaches the bundle directly. It may dangle until the first `--play`. build_app never creates
  or rewrites it: `--play` (both modes) stops before the commit phase if it is untracked, not a
  symlink, or points elsewhere (the error prints the one-line fix), and checks after the build
  that it resolves to the bundle just built.
- **Play output check:** the AAB must be signed by the upload key. Its path, SHA256,
  versionName and versionCode are printed at the end.
- The upload key must be readable by `coder_user`: give it a dedicated group, `chmod 640`,
  and the directory 750. It must not be world-readable.
- **Gradle daemon:** one per build_app run, shared by its standard/test/play gradle calls, so
  the later builds reuse the warm JVM. Its registry is private to the run
  (`.gradle-home/daemon-run.<pid>`). An EXIT trap, which also fires on INT/TERM, runs
  `./gradlew --stop` with that registry, on success or failure. That stops only this run's daemon,
  not another build_app in the same worktree and not other worktrees. If `--stop` fails,
  build_app exits non-zero; if the build had already failed, both errors are printed and the
  build's exit code is kept. A 10-minute idle timeout covers a build_app killed with SIGKILL.
- **Permissions** come from `perm-model.tsv` through `perm-lib.sh` (both ship next to
  build_app). Gradle runs under the `build-intermediates` umask (002), and the daemon started
  by the first gradle call inherits it. Afterwards `app/build` and `.gradle` get group
  ai-code, group read/write and setgid dirs; "other" bits are never stripped.
- **Deliverables** (every recorded APK, its
  `output-metadata.json`, and the AAB) are set to 644, and every directory from the worktree
  down to them gets o+rx. build_app then checks that `primary_user` can read them with no
  supplementary groups (as from a desktop session that started before a group change). If
  that check fails, build_app prints `NOT-READABLE ... blocker=<path>` and exits 1.

All keys are documented in `project.config.example`.

## deploy

`deploy` installs the last-built APKs on connected devices. It is for humans only: agents
must not run it. Like build_app, it is Android-only and kept local per project.

```
./deploy [-h|--help] [-t|--test] [-c|--clean] [-r|--rebuild] [-l|--launch] [device...] [-- gradle-flag...]
./deploy -p|--play [-n|--dry-run] [-N|--notes-file FILE | -B|--notes-base REV]
```

| Option | Effect |
|---|---|
| (none) | Install the recorded standard build on every online device |
| `-t` / `--test` | Install the recorded test build (its instrumentation APKs with `adb install -r -t`); the tag is `deployed-test`. Never falls back to the standard APKs |
| `-c` / `--clean` | Uninstall the APKs' own app id first, which removes its data (`UNINSTALL_FIRST=1`) |
| `-r` / `--rebuild` | Run `./build_app --standard` (or `--test`) first; flags after `--` go to build_app (`REBUILD=1`) |
| `-l` / `--launch` | Start the app's launcher activity after install |
| `-p` / `--play` | Upload the recorded Play bundle to Internal testing, then promote it to alpha (closed testing) and send it for review. See [deploy --play](#deploy---play) |
| `-n` / `--dry-run` | With `--play`: open an edit, read the tracks, show the notes, delete the edit; nothing uploaded or committed |
| `-N` / `--notes-file FILE` | With `--play`: release notes from FILE instead of `$EDITOR` (vi when unset); no base commit needed |
| `-B` / `--notes-base REV` | With `--play`: notes from `REV..HEAD` instead of the commit in Play's highest versionName (after a squash or history rewrite); REV must be an ancestor of HEAD |
| `tablet`, `emulator` | emulator-5554 |
| `phone` | `deploy_serial=` from project.config (error if unset) |
| other words | adb serials |

`deploy --test` refuses, before any adb call, when the test build is missing, when it is not
from HEAD, when it comes from an earlier commit than the recorded standard build, when no
standard build is recorded, or when a test build type has the standard app's id. Any recorded
APK that is missing or changed since the build fails either mode.

Before the first adb call, deploy reads every recorded APK with the SDK aapt2 (found through
`sdk.dir` in local.properties, else `ANDROID_HOME`). It stops if aapt2 is missing or fails, if `dump packagename`
does not print exactly one application id, if the APKs name different apps or versionNames,
or if the list mixes ABI-specific and universal APKs (or has more than one universal APK).
The app id always comes from the APKs. Nothing is hard-coded, so a test build with a
`.test` suffix works without extra config.

For each device it reads `ro.product.cpu.abilist` and installs the APK whose native code
matches the first ABI in that list. A universal APK (no native code) goes to every device.
After `adb install -r` it checks that the app id appears in `pm list packages` (exact
match). Any device failure leaves the deployed tag (`deployed`, or `<branch>/deployed`)
unchanged and makes deploy exit with an error. With `--test`, the instrumentation (androidTest) APKs are installed on every device after the app (`adb install -r -t`), each verified by its own package id (one APK per test package); a missing, unreadable or not-installed one fails that device and the tag stays.

Multi-user mode: run as root, it re-executes as `primary_user`. It refuses `planning_user` and `coder_user`
and needs the ai-shared and ai-code groups (`source ./orch/env`).

Per-app config:
- project-facts.md: the build declaration above.
- project.config (multi-user mode): `primary_user`, `planning_user`, `coder_user`; optional
  `deploy_serial`; `play_service_account_json` for `--play`.
- local.properties `sdk.dir`, or `ANDROID_HOME` (Android SDK with build-tools/*/aapt2).
- app/packaged-assets.py (optional): after each standard/test build, build_app runs
  `python3 app/packaged-assets.py check-apk app|instrumentation APK` on every recorded APK and
  stops on a failure. Without the file there is no check.

### deploy --play

`./build_app --play` records the signed bundle in `app/build/outputs/build_app/play.json`
(commit, path, SHA256, versionCode; written by `android-build-info stamp-play`). `deploy --play`
(`play-publish`, next to deploy) then:

1. Selects that bundle (`android-build-info select play`) and refuses it when it is missing, not
   from HEAD, or changed since the build. applicationId, versionCode and versionName come from
   the bundle's base manifest (SDK aapt2). Play is never asked for a package the bundle does not name.
2. Refuses when Play already has that versionCode on any track or as an uploaded bundle.
3. Release notes: the base commit is the `-g<hash>` in the release name of the highest versionCode
   on any track (an error when it is missing or not an ancestor of HEAD, for example after the
   history was squashed for a PR; then `--notes-base REV` names the base, and it too must be an
   ancestor of HEAD). The subjects of
   `git log --no-merges base..HEAD`, minus those matching a pattern in `play-notes-exclude`
   (shared, ships here) or the project's optional `play-notes-exclude.local` (Python regexes,
   one per line, `#` comments), open as `- subject` lines in `$EDITOR`. With EDITOR unset it is
   vi with a status line showing the live character count. Trimmed; over 500 characters (Play's
   en-US limit) reports the count, waits for Enter and reopens the editor; empty is an error.
   `--notes-file FILE` is used as is (no filtering, no base commit); empty or over 500 there is an error.
4. One edit: upload the bundle and release it to both `internal` and `alpha` (closed testing),
   status completed, same en-US notes.
5. One commit with `changesNotSentForReview=false`, so Play records a single submission that
   goes to review; a fresh read then checks that each track lists the versionCode.

`--dry-run` does everything except the upload and the commits, and deletes its edit.
Needs `python3` and `openssl` (standard library only; the JWT is signed with openssl) and
`play_service_account_json=` in project.config (absolute path, readable by primary_user).

App-specific data features (for example VehicleExpenses' `--backup`/`--restore-data`) are not
part of this script. VehicleExpenses plans to replace them with the adb export/import
provider (`~/playstore/vehicleexpenses-datatransfer-plan.md`).

## Manual screenshots

`capture-manual-screenshots` and `capture-manual-screenshots.md` ship with this build type.
Deploy copies both into an Android project root the same way it copies `build_app`. The manual
is how an agent writes that app's `manual-capture.yaml`. An app's own yaml does not live in
this directory: deploy would copy it into every Android app. David runs a capture and David
publishes this directory. Agents write the config and may run `--check`.

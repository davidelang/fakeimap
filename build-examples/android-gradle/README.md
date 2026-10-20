# build-examples/android-gradle

Build type `android-gradle`: Android apps built with Gradle. A project selects it with the line
`- **Build type:** android-gradle` in the `## Build and commit` section of its committed
project-facts.md; deploy-orchestration then installs build_app and deploy into the project root.

`build_app` here is the authoritative copy of the Android app build gateway. App repos
carry their own copy (`rules.manifest.local: local build_app`).

```
./build_app [--standard] [--test] [--play|--bundle] ["msg" | @msg_file] [files...] [-- gradle-flags]
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
| `--standard` | the standard build | `- **Standard build:**` |
| `--test` | the test build | `- **Test build:**` |
| `--play` / `--bundle` | signed Play AAB | project.config `release_keystore`, `release_key_alias`, `release_bundle_task`, `release_bundle_path` (required), `release_gradle_args` |

- Flags come first, in any order, and can be combined.
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
2. **Commit phase:** unchanged. The named files are committed, or nothing is when no message
   is given.
3. **Integrity check:** the tree must be clean. Every selected build comes from that one
   commit, including the Play build. Committing and building Play in one run is allowed.
4. **Play preflight, before any gradle work:**
   - `app/versionCode.txt` must exist, be tracked and hold one integer.
   - **Testing-version check:** a y/N question, default No. It is asked only on a branch
     with a `<branch>-start` tag, whose versionName is always `<branch>-start-<n>-g<hash>`.
     Answering No stops before anything is committed or tagged.
   - **Passwords:** `read -s`, checked with `keytool`.
   - So a long unattended stretch never stops part-way to ask.
5. **versionCode bump and release tag (Play only, automatic):**
   - `app/versionCode.txt` goes from N to N+1 and is committed alone as
     `versionCode N+1 (play build, tag T)`, by `coder_user` with the invoker's git identity.
   - That commit gets the annotated tag `T = <base>+<N+1>`. `<base>` is the nearest tag
     reachable from HEAD that is named `v<digit>…` or `<digit>…` and has no `+` in it
     (e.g. `v0.9.6` gives `v0.9.6+3`). If there is none, the base is `v0`. An existing `T`
     stops the build before the bump.
   - versionName (`git describe --always`) is therefore exactly `T`. It has no `-`, so it
     is a release, not a testing version.
   - If anything after the bump fails, `T` is deleted again and the message says so. The
     commit stays, so the next `--play` uses N+2 (Play needs increasing codes, not
     consecutive ones).
6. **Builds:** they run in sequence (standard, test, play), and the first failure stops
   everything.
7. **Tag:** the builds tag (`builds` / `<branch>/builds`) moves only after every selected
   build succeeded.

For a release: commit your work (with build_app or git), then `./build_app --play`. No
manual tag or version number is needed. To start a new version line, tag the base yourself
(`git tag -a v1.0.0`); later Play builds are then `v1.0.0+<code>`.

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
- **Play output check:** the AAB must be signed by the upload key. Its path, SHA256,
  versionName, versionCode and release tag are printed at the end.
- The upload key must be readable by `coder_user`: give it a dedicated group, `chmod 640`,
  and the directory 750. It must not be world-readable.
- **Permissions** come from `perm-model.tsv` through `perm-lib.sh` (both ship next to
  build_app). Gradle runs with `--no-daemon` under the `build-intermediates` umask (002), so
  the JVM writing the outputs inherits it. Afterwards `app/build` and `.gradle` get group
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
./deploy [-h|--help] [--test] [--clean] [--rebuild] [--launch] [device...] [-- gradle-flag...]
```

| Option | Effect |
|---|---|
| (none) | Install the recorded standard build on every online device |
| `--test` | Install the recorded test build (its instrumentation APKs with `adb install -r -t`); the tag is `deployed-test`. Never falls back to the standard APKs |
| `--clean` | Uninstall the APKs' own app id first, which removes its data (`UNINSTALL_FIRST=1`) |
| `--rebuild` | Run `./build_app --standard` (or `--test`) first; flags after `--` go to build_app (`REBUILD=1`) |
| `--launch` | Start the app's launcher activity after install |
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
and needs the ai-shared and ai-code groups (`source ./env`).

Per-app config:
- project-facts.md: the build declaration above.
- project.config (multi-user mode): `primary_user`, `planning_user`, `coder_user`; optional
  `deploy_serial`.
- local.properties `sdk.dir`, or `ANDROID_HOME` (Android SDK with build-tools/*/aapt2).

App-specific data features (for example VehicleExpenses' `--backup`/`--restore-data`) are not
part of this script. VehicleExpenses plans to replace them with the adb export/import
provider (`~/playstore/vehicleexpenses-datatransfer-plan.md`).

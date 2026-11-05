# Manual screenshots

`capture-manual-screenshots` takes user-manual JPEGs on a phone, a tablet, or both. The walk is `manual-capture.yaml` at the app root. The script is the driver. String ids and the order of steps live in the yaml. Geometry (where a node sits between two anchors, which control is the shutter, how the drawer scrim is found) stays in the script.

Deploy copies this script and this manual into an Android project root, the same way it copies `build_app`. Do not put an app's yaml in `build-examples/android-gradle/`. Deploy would install that yaml into every Android app.

David runs a capture. David publishes the script. An agent writes the yaml and may run `--check`. `--check` does not touch adb.

The device is already seeded. The tool does not create vehicles, accounts, or rows. It does not delete pictures. It may push a file named by `push_if_absent` only when that path is absent.

Do not invent screenshot files. A missing JPEG stays missing until a capture writes it. Store a locale image only when that language was photographed.

## Hooks

A hook exists for a gesture the steps cannot say. The yaml names it. The app has a Python file of functions. VehicleExpenses has no hook, and a new app should start with none.

Every hook is an audit item. Read the function and decide which of these it is:

1. Missing primitive. The gesture will show up in another app. Add the step to the shared script, then delete the hook.
2. Lazy config. The walk can already be said with the steps in this manual. Rewrite it as steps.
3. A real exception. One app, one gesture, and the primitive list would get worse if this were added. Leave the hook, and say in a comment at the function why it is an exception.

Do not add a hook to avoid writing steps. Do not add an empty hooks file. A hook that returns `False` fails that step, so `on_fail` applies. `None` and `True` succeed. Raising `StopRun` ends that device's walk.

```python
HOOKS = {"name": name}

def name(device):
    return True
```

`device` can resolve a string id (`device.s`), dump the UI, tap, swipe, press Back, and record a note. It must not delete files, force-stop the app, or tap a `never_tap` label.

```yaml
hooks: scripts/manual-capture-hooks.py
# ...
- hook: name
```

`--check` loads that file and rejects a step whose name is not in `HOOKS`.

## Command

From the app root, after this script is installed:

```
./capture-manual-screenshots --check
./capture-manual-screenshots --serial SERIAL [--form phone|tablet] [--locales all|TAG,TAG] [--shots GROUP,FILE] [--replace]
./capture-manual-screenshots --serial SERIAL --serial SERIAL [--locales all|TAG,TAG] [--shots GROUP,FILE] [--replace]
```

`--config` defaults to `manual-capture.yaml`. `--root` defaults to `.`. Before the script is installed, point `--root` at the app and run the copy under `build-examples/android-gradle/`.

One serial and no `--form`: a physical diagonal under `phone_max_inches` (default 8) is a phone. Anything else is a tablet. Two serials and no `--form`: the larger diagonal is the tablet. A gap under `gap_inches` (default 1.5) exits before any shot. `--form` with two serials is an error. The two devices run at the same time. One failure does not stop the other.

`--locales` is `all` or a comma list of pref tags. Omitted means `en` for one serial and `all` for two. `--shots` is a comma list of group names and filenames. Omitted means every shot. An unknown token exits before any shot. There is no exact pixel-size check.

Without `--replace`, a selected JPEG of at least 1000 bytes is kept, and a locale whose selected shots are all that size is not opened and its language is not changed. `--replace` overwrites the selection.

Notes go to `/tmp/manual-capture-notes-<form>.txt`.

JPEGs are max width 1080, quality 75, metadata stripped. Do not commit a full-resolution capture.

Needs `python3`, PyYAML, and Pillow. No third-party translator. No `run-as`. The walk does not force-stop the app. `language.via: android` force-stops the package only after it changes that app's Android locale.

## Config

```yaml
package: com.example.app
activity: com.example.app/.MainActivity
forms: {phone_max_inches: 8, gap_inches: 1.5}
ignore_text: ["1x", "2x", "4x"]
never_tap: [save_id, sync_now_id]
never_tap_prefix: ["Sync now"]
output:
  en: docs/user-manual/images/{form}
  other: docs/i18n/{segment}/images/{form}
endonym_key: "lang_name_{pref}"
endonym_row: {}
drawer:
  open: nav_menu
  labels: [nav_settings, nav_help]
  open_if_text: [nav_settings]
  unblock_when_same: []
language:
  via: app
  menu: nav_menu
  settings: nav_settings
  field: settings_language
  system: settings_language_system
  ready: nav_menu
launch:
  sleep: 2.5
  dismiss: []
locales:
  - {pref: en, values: values, segment: ""}
sections: []
```

`strings_res` defaults to `app/src/main/res`. English is `values/strings.xml`. For any other pref, `values` is the resource directory (`values-in`, `values-b+en+001`). When that `strings.xml` exists, it overlays English. When it does not, `--check` warns and the labels fall back to English. `--check` still requires every id the walk uses to exist in English.

`never_tap` is a list of string ids the walk must not tap (Save, Sync now, delete, clear, a background switch). `never_tap_prefix` is an English literal prefix for a label that grows (`Sync now …`). `ignore_text` drops zoom chips when the script picks a control between two anchors.

`output.en` and `output.other` are path templates. `{form}` is `phone` or `tablet`. `{segment}` and `{pref}` come from the locale row. English uses `output.en`.

`drawer.open` is the menu content-desc id. `labels` are the ids whose texts mark the open sheet. The scrim is the full-height node beside a sheet that shows at least two of those labels. `open_if_text` are more ids that mean the drawer is open. `unblock_when_same` is a pair of ids: when they translate to the same text, tapping the first is allowed even if the second is in `never_tap`.

`launch.dismiss` is tapped once if that text is visible (an onboarding skip). `launch.sleep` is seconds after start.

Back is refused while the scrim is up, and it is refused if the resumed package leaves the app.

## Locales

The locale table is in the yaml. Do not import a Python pack map. The capture has to keep working after a translation script is removed.

The first row is `en`. `pref` is the in-app language tag. `values` is the resource directory (`values` for English). `segment` is the manual path segment under `docs/i18n/`. A non-English row with an empty segment fails `--check`. Indonesian uses `values-in` when that is the directory the app ships. Brazilian Portuguese uses `values-pt` when that is the directory the app ships. Copy the app's real map. Do not guess Android's directory from the pref tag.

`endonym_key` is a format string. `{pref}` becomes the English string id of the picker row (`lang_name_en`, `lang_name_pt_br`). The bytes come from English, and they are the same in every pack. `endonym_row` is the zero-based row when the same endonym appears twice. `en-001` is the second English row, so its index is 1. Any other pref defaults to 0.

`language.via` is `app` or `android`. Omitted means `app`.

`app` opens Settings from the drawer, taps the endonym, and refuses the System default row. It restores English at the end of a run that changed the language. One locale that fails the picker is recorded and the loop continues. `menu`, `settings`, `field`, `system`, and `ready` are required, and so is `endonym_key`.

`android` is for an app that follows the device and has no language selector of its own. The driver runs `cmd locale set-app-locales` for `package`, then force-stops that app so the next launch uses the new locale. It does not open a drawer and does not change the rest of the device. The tag is the row's `android` value when set, otherwise `pref`. A row may set `android: pt-BR` when `pref` is not the Android tag. `in`, `iw`, and `ji` are sent as `id`, `he`, and `yi`. The previous app locale list is read first and restored at the end, including an empty list. `menu`, `settings`, `field`, `system`, `ready`, and `endonym_key` are not required. The walk itself still does not force-stop the app.

## Sections

A section has `group` and `shots`, then `steps`. `--shots <group>` selects every filename in sections that share that group name. Each section still runs or skips on its own shots. A filename in two groups is an error. A shot step whose file is not on a section is an error. A section shot with no shot step is an error.

A section runs only when one of its shots is selected and missing, or when `--replace` is set. A block with `shots` runs only when one of those shots is selected and missing. Omit `shots` on a block to inherit the enclosing list. `shots: []` runs whenever the parent section runs. Use that for a gesture after the JPEG is already written, such as dismissing a shutter that the shot was taken before.

Flags clear at the start of each section. A flag is set only when that step succeeds. `back_if` presses Back only when the flag is set. Two `back_if` steps, one for the form and one for the picker, press Back twice only when both opened.

`on_fail` (default `note`):

- `note` records a note and continues.
- `stop` ends this device's walk.
- `skip` leaves this step list. The rest of the enclosing block does not run. The parent continues.
- `leave` presses Back until this app is resumed, then leaves this step list.

A block's `on_fail` is the default for its own steps. A step's own `on_fail` wins. A failed shot notes that file only, unless `note_shots` names other files. `note` replaces the default message.

A step has one action. `on_fail`, `flag`, `note`, and `note_shots` sit beside it.

## Steps

`drawer: nav_fill` opens the drawer and taps that string id.

`open_drawer: true` and `close_drawer: true` only move the drawer. Close taps the scrim. A failed close after the drawer shot should be `on_fail: stop`. Later sections may open the drawer again.

`tap_text` taps a string id. A string value is the id. A mapping can set `scroll` to `page` (one page, then one more try) or `fling` (until the bottom labels stop changing, at most eight). `else` is one more id, or a list, tried in order.

```yaml
- tap_text:
    id: reports_expenses_list
    else: [nav_expense_list]
    scroll: page
```

`tap_desc: some_id` taps that content description.

`tap_field: some_id` taps the clickable on that label, or the label itself.

`tap_between` taps the clickable strictly between two content-desc anchors. `1x`, `2x`, and `4x` are ignored. This is the shutter between Save and Pick, and the shutter between the two start-trip labels.

```yaml
- tap_between:
    desc: [fuel_save, fill_pick_an_existing_image]
  on_fail: skip
```

`tap_beside` is the pump control. Portrait: on the shutter row, to the right of Pick. Landscape: above the shutter. `save` and `pick` are the anchor ids. `skip` is more ids that are not the control.

`tap_matching` taps text. `contains` is a literal substring (a gallery filename such as `not-a-dash`, which is not a string id). `card: true` requires a middle dot and `yyyy-mm-dd`, which is how an expense or fill card titles its row. `skip_format` is the string id of a line that must not be taken for that card (the fills-count line). It does not scroll. Put it after the screen that shows the row.

`tap_any` taps the first visible literal. These are device-language words, not string ids (`Pictures`, `Albums`, `Folders`, `Collections`). `scroll` is `page` or `fling`. `backup` is a list of steps tried only when the first tap misses. A backup step that fails fails the whole `tap_any`. Backup does not use `on_fail`.

```yaml
- tap_any:
    text: [Pictures]
    scroll: page
    backup:
      - tap_any:
          text: [Albums, Folders, Collections]
      - wait: 0.7
      - tap_any:
          text: [Pictures]
          scroll: page
```

`swipe: page` or `swipe: fling` is one gesture.

`wait: 0.6` sleeps that many seconds.

`shot: file.jpg` writes that JPEG when it is selected. A mapping sets `file` and `unless`. `unless` is a list of string ids. If one of those texts is on screen, the file is not written, a note is recorded, and the walk continues. Use that when the screen is still the English failure or the "not identified" dialog.

`back: 1` presses Back that many times. `back_if: form` presses Back once when that flag was set. `back_to_app` presses Back until this package is resumed. `back_while_dialog` presses Back while the name list is up, at most twice. It needs `dialog` and `show_all` string ids. It does not tap a name.

`wait_until` waits until that name list is up. `timeout` is seconds (default 25). The name list is the title plus a clickable name that is not pack chrome. Show-all is not a name.

`wait_settle` waits out the spinner after a shutter. `kind` is `fill` or `trip`. `processing` and `retry` are string ids. `dialog` and `show_all` are the name-list ids. `between` is the two shutter anchors. `timeout` defaults to 20. A timeout still lets the walk continue.

`push_if_absent` pushes a small gray JPEG only when `remote` is missing, then asks the media scanner to see it. It does not delete.

`hook: name` calls `HOOKS["name"]`.

`block` groups steps and names the shots that justify them. `on_fail: leave` on the block becomes the default for those steps, so a gallery failure can return to the app without skipping the later shutter shots.

Take the live shot before the shutter. The shutter, the settle, and Back only dismiss the dialog. The start-trip JPEG is the live screen.

```yaml
- shot: start-trip.jpg
- block:
    shots: []
    steps:
      - tap_between:
          desc: [nav_start_trip, trip_personal_now_at_this_location]
        on_fail: skip
        note_shots: [start-trip.jpg]
      - wait_settle:
          kind: trip
          processing: ui_cancel_processing
          retry: settings_retry
          dialog: vehicle_vehicle_not_identified
          show_all: vehicle_show_all_vehicles
          between: [nav_start_trip, trip_personal_now_at_this_location]
- back: 1
```

A nested block whose shots are a subset keeps the parent from opening a screen that was not selected. A failed card tap uses `on_fail: note` and `note_shots` so the list photo still happens.

## What an agent does

Write `manual-capture.yaml` at the app root. Point every string id at English `strings.xml`. Put the locale table in the yaml. Run `--check` and read the `ok N shots, N locales` line. A missing English id fails the check. A missing locale overlay warns.

Do not run a capture unless David asks. Do not commit full-resolution shots. Do not delete an older capture script unless David asks. Do not add a hook unless the three-way audit above says it is a real exception. Do not run `./deploy-orchestration`.

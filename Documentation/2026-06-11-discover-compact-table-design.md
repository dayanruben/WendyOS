# Compact device table for `wendy discover` and device pickers

## Problem

The shared device table (`pickerDeviceColumnDefs` in `go/internal/cli/tui/picker.go`,
used by `wendy discover` and the device pickers) is ~132 columns wide at minimum.
Most of the width comes from verbose headers ("wendy-agent version" min 20,
"WendyOS Version" min 16, "Provisioned" min 13) and an always-rendered
Description column (min 20) that is empty in discover output.

## Design

Target layout (~80 columns):

```
     Name            Type       Address        Agent     OS      P
 ★   wendy-jetson    USB, LAN   wendy.local    1.2.3 ⚠   12.4    ●
     pi5-dev         LAN        10.0.1.5       1.2.4     12.4    ○
     esp32-c6        ESP32      /dev/tty.usb1

  ● provisioned  ○ unprovisioned  ⚠ agent older than CLI
```

Changes, all in the shared column definitions so discover and pickers stay
consistent:

1. **Short headers.** "wendy-agent version" → "Agent" (minWidth 20 → 7),
   "WendyOS Version" → "OS" (minWidth 16 → 4).
2. **Provisioned glyph column.** Header "P", minWidth 3. Values: `●` for
   provisioned, `○` for unprovisioned, empty when unknown (non-LAN devices).
   The `PickerItem.Provisioned` string field keeps its current
   "Provisioned"/"Unprovisioned"/"" semantics; only the rendered cell changes.
   The clipboard JSON (`discoverDeviceInfo.Provisioned`) keeps the full word.
3. **Auto-hide empty Description.** Add an `optional` flag to
   `pickerColumnDef`: an optional column is hidden when no item has a value,
   even in fixed-column mode. Only Description in the device defs is marked
   optional. Discover never sets Description, so it never appears there; static
   picker lists that set it still show it. Because discover items never gain a
   Description mid-scan, the column cannot pop in/out during continuous
   refresh.
4. **Legend line.** A dim legend rendered under the table in both the discover
   TUI and the device picker: `● provisioned  ○ unprovisioned  ⚠ agent older
   than CLI`. Shown whenever the table has rows.

Out of scope: `cloud_discover.go` has its own table (`discoverTableColumns`)
and is unchanged. Connection-type glyphs were considered and rejected (chosen
option keeps "USB, LAN" text).

## Testing

Update `picker_test.go` / `discover_test.go` expectations for the new headers,
glyph values, and hidden Description column; add coverage for the
optional-column behavior (hidden when empty, shown when any item sets it).

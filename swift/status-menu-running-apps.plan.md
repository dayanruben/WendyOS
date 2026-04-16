# Status menu running apps plan

## Goal

Show currently running apps in the macOS menu bar app using the existing
`WendyAgent.apps` observation stream and the current `WendyAppInfo.id` as the
visible app label.

## Desired menu structure

1. Agent status item
2. Any existing status failure detail rows
3. Separator
4. Disabled section header: `Running Apps`
5. One menu item per running app
6. Separator
7. Quit item

If there are no running apps, keep the section visible and show a single
disabled `None` row under `Running Apps`.

## App row behavior

Each running app should appear as a top-level menu item titled with
`WendyAppInfo.id`.

Each app item should have a submenu with disabled detail rows:

- `ID: <id>`
- `Kind: Native` or `Kind: Container`
- `Status: Running`
- `PID: <pid>` or `PID: Unknown`

## Files likely involved

- `WendyAgentMac/Sources/StatusMenuController.swift`
- optionally a small presentation helper if the controller becomes too noisy

## Implementation plan

### 1. Observe app updates in `StatusMenuController`

Add stored state:

- `currentApps: [WendyAppInfo] = []`
- `appsObservation: WendyObservation?`

Subscribe with `wendyAgent.observeApps(...)` in the initializer, mirroring the
existing status observation.

On every app update:

- store the latest app snapshot
- rebuild the menu

## 2. Filter to currently running apps

Add a helper that derives the running apps list from `currentApps`:

- include only apps whose `status == .running`
- sort by `id` for stable menu ordering

## 3. Reshape menu building order

Update `rebuildMenu()` so the running apps section appears between the status
block and the quit item.

Preserve current status presentation and existing failure detail rows.

## 4. Build one top-level menu item per running app

For each running app:

- create an `NSMenuItem` titled with `app.id`
- attach a submenu created from that app’s details

## 5. Build per-app submenus

Add a helper that creates a submenu containing disabled, informational rows for
that app’s ID, kind, status, and PID.

## 6. Handle the empty state

If there are no running apps:

- add the disabled `Running Apps` header
- add a disabled `None` row below it

## 7. Cancel both observations on quit

When quitting the app, cancel:

- the existing status observation
- the new apps observation

A combined cleanup helper is fine if that simplifies shutdown.

## Validation

Manual checks:

1. No running apps -> section shows `Running Apps` and `None`
2. One running app -> app appears with submenu details
3. Multiple running apps -> rows are stable and sorted by ID
4. App exits on its own -> row disappears automatically
5. Quit -> both observations are cancelled and the agent still stops cleanly

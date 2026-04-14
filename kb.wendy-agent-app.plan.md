# WendyAgentApp AppKit Menu Migration Plan

## Goal

Replace the current SwiftUI `MenuBarExtra`-based menu with a native AppKit status bar item and `NSMenu` so the app uses standard macOS menu rendering and supports reliable menu item icons/state presentation.

This migration should follow these constraints:

- AppKit only for the menu bar UI
- all UI created in code
- no Auto Layout
- no storyboards
- no xibs
- classic MVC
- no View Models

## Why switch

The current implementation in:

- `swift/WendyAgentApp/WendyAgentApp.swift`
- `swift/WendyAgentApp/WendyAgentMenu.swift`

uses `MenuBarExtra` for both the menu bar item and the menu contents. In practice, the menu content is constrained by native menu rendering, and our attempts to render custom SwiftUI elements such as:

- overlays
- custom badges
- colored status dots
- more complex stacked layouts

have been unreliable or ignored.

If the requirement is "native Mac menus", AppKit is the right foundation:

- `NSStatusItem` for the menu bar item
- `NSMenu` for the popup menu
- `NSMenuItem` for rows
- optional template/status images for icons

## Target architecture

### 1. Use a code-only AppKit app lifecycle

Move the menu bar app lifecycle to AppKit instead of keeping the SwiftUI `App`
entry point around as glue.

Suggested new file:

- `swift/WendyAgentApp/AppDelegate.swift`

Suggested type:

- `@main final class AppDelegate: NSObject, NSApplicationDelegate`

Responsibilities:

- own the `WendyAgent`
- own the current `WendyAgentStatus`
- start and stop observation
- create the menu controller
- forward status updates into the menu controller
- terminate the app cleanly

This keeps the app in a classic AppKit shape and avoids mixing SwiftUI app
lifecycle concerns into an otherwise native menu implementation.

### 2. Replace `MenuBarExtra` with an AppKit controller

Introduce an AppKit controller responsible for:

- creating the `NSStatusItem`
- configuring its button image/title
- creating and rebuilding the `NSMenu`
- responding to menu actions such as Quit
- updating the menu whenever `WendyAgentStatus` changes

Suggested new file:

- `swift/WendyAgentApp/StatusMenuController.swift`

Suggested type:

- `final class StatusMenuController: NSObject`

Core responsibilities:

- hold `NSStatusItem`
- hold current `WendyAgentStatus`
- expose `func update(status: WendyAgentStatus)`
- expose `func setQuitHandler(_:)`
- rebuild menu contents when status changes

### 3. Stop using SwiftUI for the popup menu contents

`WendyAgentMenu.swift` should be removed entirely.

The actual menu contents should be built with AppKit:

- `NSMenu`
- `NSMenuItem`
- separators

Example target menu structure:

1. status item row
   - title: `Idle`, `Starting`, `Running`, `Stopping`, `Stopped`, or `Failed`
   - icon/image indicating status category
2. if failed:
   - disabled detail row(s) showing the error message
3. separator
4. `Quit WendyAgent`

## Architectural rules

### Code-only UI

Do not use:

- storyboards
- xibs
- Interface Builder

All AppKit objects should be created in code.

For this menu-based app that means:

- create `NSStatusItem` in code
- create `NSMenu` in code
- create every `NSMenuItem` in code
- if any custom `NSView` is ever needed, instantiate it directly in code

### No Auto Layout

Do not introduce Auto Layout constraints for this migration.

For the current plan, that is straightforward because `NSStatusItem` and `NSMenu`
mostly rely on AppKit's native sizing behavior. If a custom `NSView` is added
later, size and position it with explicit frames and manual layout code.

### Classic MVC only

Use classic AppKit MVC:

- **Model**: `WendyAgent`, `WendyAgentStatus`, and related domain state
- **View**: `NSStatusItem`, `NSMenu`, `NSMenuItem`, optional `NSImage`
- **Controller**: `AppDelegate`, `StatusMenuController`, and any small action
  handlers

Do not add a View Model layer. Presentation decisions such as status title,
icon name, or whether an error message should be shown can live either:

- in the controller as private helpers, or
- in a small internal extension on `WendyAgentStatus`

The app is simple enough that adding View Models would create extra indirection
without solving a real problem.

## Native menu representation strategy

Because `NSMenu` is row-based and not arbitrary-layout based, do not try to reproduce the current custom SwiftUI layout exactly.

Instead, use standard native menu idioms.

### Status row

Represent status as a normal disabled menu item.

Suggested mappings:

- `idle` → `Idle`
- `starting` → `Starting`
- `running` → `Running`
- `stopping` → `Stopping`
- `stopped` → `Stopped`
- `failed` → `Failed`

Mark the item disabled so it behaves like informational text, not an action.

### Status color / icon

There are two AppKit-friendly options.

#### Option A: status template images in assets

Create small status icons as assets, for example:

- `StatusMenuGray`
- `StatusMenuYellow`
- `StatusMenuGreen`
- `StatusMenuRed`

Use them as `NSMenuItem.image`.

Pros:

- most predictable in native menus
- easy to visually match System Settings
- no reliance on attributed title rendering

Cons:

- requires adding assets

#### Option B: SF Symbols where appropriate

Use symbols such as:

- `circle.fill`
- `exclamationmark.circle.fill`

with configured symbol images if rendering is acceptable.

Pros:

- quick to prototype

Cons:

- color/tint behavior in `NSMenuItem.image` can be inconsistent depending on rendering mode
- less predictable than dedicated assets

### Recommendation

Use **dedicated menu status dot assets** for reliability.

## Error message handling

For `.failed(let message)`, do not attempt a complex wrapped custom row first.

Use one of these native patterns:

### Preferred

Add one or more disabled menu items below `Failed`.

For long messages:

- either truncate to a concise summary in the menu
- or split into a couple of disabled lines

Example:

- `Failed`
- `Connection lost`
- separator
- `Quit WendyAgent`

### Alternative

Use an alert or separate window for detailed diagnostics later if needed.

For this migration, keep the menu simple.

## Status bar button strategy

The top menu bar item itself should also move to AppKit.

### Initial approach

Use the existing asset:

- `StatusIcon`

Set it on:

- `statusItem.button?.image`

Configure as template if needed so it behaves correctly in the menu bar.

### Error indication in the menu bar item

Do not attempt overlay composition in AppKit button title/image on day one.

Instead choose one of:

1. plain base icon only
2. swap between two full images:
   - `StatusIcon`
   - `StatusIconError`
3. append a text marker in the button title if acceptable

### Recommendation

Use **two complete assets** if a distinct error state is needed in the menu bar item itself.

## Concrete implementation steps

### Step 1: add an AppKit app delegate

Create:

- `swift/WendyAgentApp/AppDelegate.swift`

Implement:

- `@main final class AppDelegate: NSObject, NSApplicationDelegate`
- `private let agent = WendyAgent()`
- `private var status: WendyAgentStatus = .idle`
- `private var statusObservation: WendyObservation?`
- `private var isQuitting = false`
- `private let statusMenuController = StatusMenuController(status: .idle)`

Methods:

- `func applicationDidFinishLaunching(_:)`
- `func applicationWillTerminate(_:)` if needed
- `private func bootstrapIfNeeded()`
- `private func updateStatus(_:)`
- `@objc private func quitSelected()` or similar quit entry point

This file should act as the top-level application controller in a classic MVC
setup.

### Step 2: add the status menu controller

Create:

- `swift/WendyAgentApp/StatusMenuController.swift`

Implement:

- `final class StatusMenuController: NSObject`
- `private let statusItem: NSStatusItem`
- `private var onQuit: (() -> Void)?`
- `private var currentStatus: WendyAgentStatus`

Methods:

- `init(status: WendyAgentStatus)`
- `func update(status: WendyAgentStatus)`
- `func setQuitHandler(_ handler: @escaping () -> Void)`
- `private func rebuildMenu()`
- `private func updateStatusButton()`
- `@objc private func quitSelected()`

Keep this controller focused on AppKit menu presentation and user actions.

### Step 3: build native menu items in code

Inside `rebuildMenu()`:

- create fresh `NSMenu`
- append a disabled status item with title from a shared mapping helper
- assign `image` from status category
- if failed, append disabled detail item(s)
- append separator
- append Quit item with action/target
- assign menu to `statusItem.menu`

Do not use nib-backed menu items or custom views loaded from Interface Builder.

### Step 4: keep presentation helpers small and local

To avoid duplication, define small internal helpers either in the controller or a
separate file:

- `var menuTitle: String`
- `var menuImageName: String`
- `var isTransitional: Bool`

If convenient, add an internal extension on `WendyAgentStatus`.

Suggested file if extracted:

- `swift/WendyAgentApp/WendyAgentStatus+MenuPresentation.swift`

This helper is still part of the model/presentation boundary. It is **not** a
View Model.

### Step 5: remove the SwiftUI app entry point and menu views

After AppKit is wired up:

- remove `MenuBarExtra`
- remove the SwiftUI `App` entry point
- delete `WendyAgentMenu`
- delete `WendyAgentStatusItem`

Files likely removed or replaced:

- `swift/WendyAgentApp/WendyAgentApp.swift`
- `swift/WendyAgentApp/WendyAgentMenu.swift`

### Step 6: add assets for menu status icons

Add new assets under:

- `swift/WendyAgentApp/Assets.xcassets`

Suggested image sets:

- `MenuStatusIdle`
- `MenuStatusTransition`
- `MenuStatusRunning`
- `MenuStatusFailed`

These should be tiny dot icons optimized for `NSMenuItem.image`.

## Proposed status mapping

| WendyAgentStatus | Menu title | Menu icon |
|---|---|---|
| `.idle` | `Idle` | gray dot |
| `.starting` | `Starting` | yellow dot |
| `.running` | `Running` | green dot |
| `.stopping` | `Stopping` | yellow dot |
| `.stopped` | `Stopped` | gray dot |
| `.failed(_)` | `Failed` | red dot |

## Suggested rollout order

1. create `AppDelegate` and switch the app lifecycle to AppKit
2. create `StatusMenuController`
3. wire `NSStatusItem` with a static menu and Quit action
4. connect live status updates
5. add native status row and failure detail row
6. remove `MenuBarExtra` and the SwiftUI menu views
7. add status icon assets for menu rows
8. optionally add alternate menu bar icon for error state

## Risks / tradeoffs

### Pros

- truly native macOS menu behavior
- predictable rendering
- easier to maintain for simple status menus
- no more fighting `MenuBarExtra` rendering limitations

### Cons

- less declarative than SwiftUI
- `NSMenu` is less flexible for custom layout
- richer visuals require assets instead of view composition

## Recommendation summary

Proceed with an AppKit migration based on:

- `NSApplicationDelegate` as the app entry point
- `NSStatusItem`
- `NSMenu`
- asset-backed status dot icons
- simple disabled status rows
- code-only UI construction
- no Auto Layout
- classic MVC with no View Models

This fits the product direction better than continuing to push custom SwiftUI layouts through `MenuBarExtra`.

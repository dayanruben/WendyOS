# Wendy Agent macOS onboarding plan

## Goal

On first launch, show a welcome/setup window before prompting for Bluetooth, camera, or microphone permissions.

The onboarding should:
- explain what Wendy Agent is doing
- guide the user through the upcoming permission prompts
- let the user control whether Wendy Agent launches automatically at login
- feel like a setup flow, not an optional info dialog

The agent itself should still start immediately on launch. Onboarding is only for setup and permission prompting.

---

## Architectural decisions

### AppKit stays responsible for app coordination
- `WendyAgentApplication.swift` remains the app entry point.
- `AppDelegate.swift` remains the composition root.
- `StatusMenuController.swift` remains AppKit.

### SwiftUI is used only for onboarding UI and onboarding state
- The onboarding flow will be hosted in an `NSWindow` created by `AppDelegate`.
- The window content will be an `NSHostingController` wrapping a single SwiftUI view.
- Onboarding flow/state/permission logic will live in a single SwiftUI-facing object named `Onboarding`.

### KISS rules
- No `AppController`
- No onboarding presenter
- No separate preferences wrapper type
- No separate permission service
- No onboarding-specific logic in `StatusMenuController`

---

## Ownership and responsibilities

### `AppDelegate`
Owns:
- `WendyAgent`
- `StatusMenuController`
- onboarding `NSWindow`
- a single `Onboarding` instance

Responsibilities:
- start `WendyAgent` immediately in `applicationDidFinishLaunching`
- create the status menu controller
- show onboarding on first launch
- reopen onboarding when requested from the status menu
- close and nil out the onboarding window when dismissed
- implement `StatusMenuControllerDelegate`

### `StatusMenuController`
Owns:
- the menu bar item
- menu contents and menu UI only

Responsibilities:
- display Wendy Agent status and running apps
- add a `Welcome & Permissions…` menu item
- relay actions that are outside its scope to a delegate

It should not:
- create windows
- know about onboarding implementation details
- quit the app directly
- know about `AppDelegate` internals

### `StatusMenuControllerDelegate`
Add a delegate protocol so `StatusMenuController` can forward actions upward.

Suggested API:
- `statusMenuControllerDidSelectWelcomeAndPermissions(_:)`
- `statusMenuControllerDidSelectQuit(_:)`

`AppDelegate` will implement this protocol.

### `Onboarding`
A single `@MainActor` observable object, likely:
- `final class Onboarding: NSObject, CBCentralManagerDelegate`

Responsibilities:
- own the onboarding step/state machine
- own the launch-at-login toggle state
- read/write onboarding state directly with `UserDefaults`
- request Bluetooth/camera/microphone permissions
- expose view data like title, message, button labels, permission statuses
- mark onboarding as completed when the user presses Done

Bluetooth support lives here too, since it needs `CBCentralManagerDelegate` and continuation storage.

### `OnboardingView`
A single SwiftUI view hosted inside the onboarding window.

Responsibilities:
- render the entire flow in one stable layout
- show dynamic title/message/checklist based on `onboarding.step`
- show the login-item toggle on the welcome step
- show `Later` and `Continue` during setup
- show `Done` on completion
- call into `Onboarding` for progression
- call a `closeWindow` closure provided by `AppDelegate`

---

## Window behavior

`AppDelegate` should create and manage one onboarding window.

Behavior:
- if onboarding is already open, bring it to front instead of creating another window
- use `NSApp.activate(ignoringOtherApps: true)` before showing it
- keep the app as accessory/AppKit-based otherwise
- nil out the stored window reference when the window closes

Suggested window characteristics:
- titled
- closable
- centered
- fixed onboarding size
- SwiftUI content hosted via `NSHostingController`

---

## First-launch behavior

On app launch:
1. `AppDelegate` creates `StatusMenuController`
2. `AppDelegate` starts `WendyAgent` immediately
3. `AppDelegate` checks whether onboarding has already been completed via `UserDefaults`
4. if not completed, `AppDelegate` shows the onboarding window

Important:
- permissions are **not** requested automatically from `applicationDidFinishLaunching`
- permission prompts are only triggered as the user advances through onboarding
- login-item registration is also user-driven from onboarding

---

## Onboarding flow

The onboarding should be a single-window, single-view flow driven by a step enum.

Suggested steps:
- `welcome`
- `bluetooth`
- `camera`
- `microphone`
- `complete`

### Welcome step
Purpose:
- introduce Wendy Agent
- explain that setup will request permissions
- let the user choose launch-at-login behavior

UI:
- title: `Welcome to Wendy Agent`
- body explaining that setup will ask for hardware permissions so Wendy apps can use Bluetooth, camera, and microphone
- checklist showing Bluetooth, camera, and microphone
- toggle: `Open Wendy Agent automatically when you log in`
- buttons: `Later` and `Continue`

Action on Continue:
- persist launch-at-login preference via `UserDefaults`
- apply login-item registration/unregistration
- advance to Bluetooth step

### Bluetooth step
Purpose:
- prepare the user for the Bluetooth system prompt

UI:
- title: `Allow Bluetooth Access`
- explanation of why Wendy Agent needs Bluetooth
- checklist remains visible
- buttons: `Later` and `Continue`

Action on Continue:
- request Bluetooth permission
- update Bluetooth status
- advance to camera step

### Camera step
Purpose:
- prepare the user for the camera system prompt

UI:
- title: `Allow Camera Access`
- explanation of why Wendy Agent needs camera access
- buttons: `Later` and `Continue`

Action on Continue:
- request camera permission
- update camera status
- advance to microphone step

### Microphone step
Purpose:
- prepare the user for the microphone system prompt

UI:
- title: `Allow Microphone Access`
- explanation of why Wendy Agent needs microphone access
- buttons: `Later` and `Continue`

Action on Continue:
- request microphone permission
- update microphone status
- advance to complete step

### Complete step
Purpose:
- close the setup flow positively

UI:
- title: `Setup Complete`
- short success message
- button: `Done`

Action on Done:
- mark onboarding complete in `UserDefaults`
- close the onboarding window

---

## Tone and copy direction

The onboarding should gently nudge users to complete setup.

Copy should:
- frame the flow as finishing Wendy Agent setup
- explain benefits in concrete terms
- avoid telling users the permissions are optional
- avoid language that discourages completion

Recommended tone:
- welcoming
- direct
- setup-oriented
- concise

Recommended labels:
- `Welcome & Permissions…` for the status menu item
- `Open Wendy Agent automatically when you log in` for the toggle
- `Later` for the secondary action during setup
- `Continue` for step progression
- `Done` for completion

---

## `UserDefaults` usage

Do not create a dedicated preferences wrapper type.

Use direct `UserDefaults.standard` access from `Onboarding`, with static key constants inside `Onboarding`.

Suggested keys:
- `hasSeenOnboarding`
- `launchAtLoginEnabled`

Behavior:
- if `launchAtLoginEnabled` is unset, default it to `true`
- `hasSeenOnboarding` controls whether first-launch onboarding is shown

---

## Permission handling details

### Bluetooth
Handled in `Onboarding`.

Implementation notes:
- `Onboarding` conforms to `CBCentralManagerDelegate`
- instantiate `CBCentralManager` only when the Bluetooth step continues
- use a stored continuation to bridge delegate callback to async flow

### Camera
Handled in `Onboarding` via:
- `AVCaptureDevice.authorizationStatus(for: .video)`
- `AVCaptureDevice.requestAccess(for: .video)`

### Microphone
Handled in `Onboarding` via:
- `AVCaptureDevice.authorizationStatus(for: .audio)`
- `AVCaptureDevice.requestAccess(for: .audio)`

### Status display
Each permission row in the single onboarding view should show a status such as:
- pending
- next/current
- allowed
- denied
- restricted

This makes the flow understandable while keeping everything in one view.

---

## Launch at login behavior

The login-item toggle belongs on the welcome step.

When the user presses Continue from the welcome step:
- save the toggle state to `UserDefaults`
- if enabled, call `SMAppService.mainApp.register()` as needed
- if disabled, call `SMAppService.mainApp.unregister()` as needed

This avoids silently enabling launch-at-login before the user sees the setup screen.

---

## Status menu changes

Update `StatusMenuController` to:
- take a `StatusMenuControllerDelegate`
- add a `Welcome & Permissions…` menu item
- relay that action to the delegate
- relay Quit to the delegate as well

This keeps `StatusMenuController` scoped to menu UI only.

---

## File-level plan

### Modify
- `WendyAgentMac/Sources/AppDelegate.swift`
- `WendyAgentMac/Sources/StatusMenuController.swift`

### Add
- `WendyAgentMac/Sources/StatusMenuControllerDelegate.swift` (or define protocol near `StatusMenuController`)
- `WendyAgentMac/Sources/Onboarding.swift`
- `WendyAgentMac/Sources/OnboardingView.swift`

---

## Expected end state

After implementation:
- Wendy Agent starts immediately on launch
- first launch shows a welcome/setup window before any permission prompt appears
- camera/microphone/Bluetooth prompts only appear when the user advances through onboarding
- the user can choose launch-at-login before it is configured
- the status menu can reopen onboarding later
- onboarding is implemented as one SwiftUI view backed by one `Onboarding` object
- `AppDelegate` remains the coordinator and `StatusMenuController` remains AppKit-only

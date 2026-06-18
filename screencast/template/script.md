# Feature Name

Audience: Engineering peers who want to understand the feature and how it changes
their workflow.
Goal: Show the smallest useful story: what problem exists, what the feature does,
how it feels in practice, and what the viewer should do next.
Tone: Calm, technical, direct. Show working software. Avoid hype.

---

## 01 Problem

### Say

Describe the problem in the viewer's terms. Keep this grounded in the workflow,
not implementation details.

### Show (diagram)

A simple visual that makes the pain obvious, for example:

```text
Current workflow
        ↓
manual step / slow feedback / fragile setup
        ↓
more time before the developer can verify the change
```

---

## 02 What Changed

### Say

Explain the feature in one or two paragraphs. Name the user-visible behavior and
why it solves the problem.

### Show (slide)

A concise config, command, UI state, or diagram that introduces the feature.

```text
Before: extra manual work
After:  one normal workflow with the feature built in
```

---

## 03 Developer Flow

### Say

Walk through the happy path from the developer's point of view. Emphasize what
they do, what they see, and what no longer needs to happen.

### Show (terminal)

Show the core workflow. Keep the terminal focused and short.

```sh
wendy run --device example-device
```

---

## 04 Runtime / Result

### Say

Explain the result after the workflow completes. Focus on the observable state:
what is running, where files are, what changed, or how the app behaves.

### Show (code)

A verification command, app snippet, screenshot, or result summary.

---

## 05 Safety / Scope

### Say

Describe important boundaries, unsupported cases, or safety rules. Be explicit
about what fails intentionally.

### Show (slide)

A short list or examples of valid and invalid usage.

---

## 06 Why This Matters

### Say

Connect the feature back to the product story and developer value.

### Show (slide)

Final summary slide:

```text
Feature Name

✓ Benefit one
✓ Benefit two
✓ Benefit three
✓ Built into the normal workflow
```

---

## 07 Closing

### Say

End with the simplest pitch and the next step for the viewer.

### Show (slide)

A short call to action or contact note.

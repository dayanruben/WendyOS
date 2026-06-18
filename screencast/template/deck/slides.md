---
theme: default
colorSchema: dark
title: Feature screencast
info: |
  Generated from scene folders. Edit scenes/*/slide.md and rerun scripts/build-scenes.mjs.
class: text-left
drawings:
  persist: false
transition: fade-out
mdc: true
---

---

# Feature screencast

One sentence describing what changed and why it matters.

- Audience: engineering peers
- Goal: explain the feature workflow
- Output: narrated MP4 and manually presentable deck

<!--
Scene id: 01-intro
Voiceover: scenes/01-intro/voice.md
Scene: scenes/01-intro
-->

---

# Problem

What was hard, slow, risky, or confusing before this change?

<!--
Scene id: 02-problem
Voiceover: scenes/02-problem/voice.md
Scene: scenes/02-problem
-->

---

# Demo

<video :src="'/scenes/03-demo/video.mp4'" controls muted width="100%"></video>

<!--
Scene id: 03-demo
Voiceover: scenes/03-demo/voice.md
Scene: scenes/03-demo
-->

---

# Result

What is now possible?

- Outcome one
- Outcome two
- Boundary or caveat

<!--
Scene id: 04-result
Voiceover: scenes/04-result/voice.md
Scene: scenes/04-result
-->

---

# Thanks

Questions?

<!--
Scene id: 05-thanks
Voiceover: scenes/05-thanks/voice.md
Scene: scenes/05-thanks
-->

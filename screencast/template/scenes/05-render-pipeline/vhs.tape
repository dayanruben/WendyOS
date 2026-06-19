Output vhs.tape.mp4
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false

Sleep 1s

Type "cd screencast"
Enter
Sleep 700ms

Type "scripts/render-slide 01"
Enter
Sleep 800ms

Type "scripts/render-voice 01"
Enter
Sleep 800ms

Type "scripts/render-tape --dry-run --with-hooks 02"
Enter
Sleep 800ms

Type "scripts/render-tape --with-hooks 02"
Enter
Sleep 800ms

Type "scripts/stitch scenes/* --output output/feature-name.mp4"
Enter
Sleep 2s

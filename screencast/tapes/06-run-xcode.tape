Output deck/public/videos/mac-beta/06-run-xcode.mp4
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false

Hide
Type "cd $(git rev-parse --show-toplevel)"
Enter
Ctrl+L
Show

Sleep 1s

Type "cd Examples/HelloMLX"
Enter
Sleep 500ms

Type "ls"
Enter
Sleep 1s

Type "xcodebuild -find metal"
Enter
Sleep 2s

Type "wendy run --device mac-mini.local:50051 --detach"
Enter
Sleep 10s

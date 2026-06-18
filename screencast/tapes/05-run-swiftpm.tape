Output deck/public/videos/mac-beta/05-run-swiftpm.mp4
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

Type "cd Examples/HelloMac"
Enter
Sleep 500ms

Type "wendy run --device mac-mini.local:50051 --detach"
Enter
Sleep 8s

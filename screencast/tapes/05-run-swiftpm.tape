Output deck/public/videos/mac-beta/05-run-swiftpm.mp4
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false

Sleep 1s

Type "export WENDY_MAC_TARGET=mac-mini.local:50051"
Enter
Sleep 500ms

Type "cd $(git rev-parse --show-toplevel)/Examples/HelloMac"
Enter
Sleep 500ms

Type "wendy run --device $WENDY_MAC_TARGET --build-type swift"
Enter
Sleep 12s

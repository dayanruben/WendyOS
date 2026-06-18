Output deck/public/videos/mac-beta/06-run-xcode.mp4
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

Type "cd $(git rev-parse --show-toplevel)"
Enter
Sleep 500ms

Type "echo 'MLX and VLMMLX style apps use Xcode, an app bundle, and Metal.'"
Enter
Sleep 1s

Type "ls -d Examples/HelloMLX/HelloMLX.xcodeproj"
Enter
Sleep 1s

Type "xcodebuild -find metal"
Enter
Sleep 2s

Type "cd Examples/HelloXcode"
Enter
Sleep 500ms

Type "ls -d *.xcodeproj"
Enter
Sleep 1s

Type "wendy run --device $WENDY_MAC_TARGET"
Enter
Sleep 14s

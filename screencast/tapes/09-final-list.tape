Output deck/public/videos/mac-beta/09-final-list.mp4
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

Type "wendy --device $WENDY_MAC_TARGET device apps list"
Enter
Sleep 3s

Type "printf '\nBeta boundary: native SwiftPM and Xcode apps on Apple Silicon Macs; no Linux or WendyOS containers, mTLS provisioning, or cloud support in this preview.\n'"
Enter
Sleep 4s

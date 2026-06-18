Output deck/public/videos/mac-beta/07-app-lifecycle.mp4
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

Type "export WENDY_APP_ID=sh.wendy.examples.HelloMac"
Enter
Sleep 500ms

Type "wendy --device $WENDY_MAC_TARGET device apps list"
Enter
Sleep 3s

Type "wendy --device $WENDY_MAC_TARGET device apps stop $WENDY_APP_ID"
Enter
Sleep 3s

Type "wendy --device $WENDY_MAC_TARGET device apps remove --force $WENDY_APP_ID"
Enter
Sleep 3s

Type "wendy --device $WENDY_MAC_TARGET device apps list"
Enter
Sleep 3s

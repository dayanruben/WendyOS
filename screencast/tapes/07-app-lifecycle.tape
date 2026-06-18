Output deck/public/videos/mac-beta/07-app-lifecycle.mp4
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false

Sleep 1s

Type "wendy --device mac-mini.local:50051 device apps list"
Enter
Sleep 5s
Type "q"
Sleep 500ms

Type "wendy --device mac-mini.local:50051 device apps stop sh.wendy.examples.HelloMac"
Enter
Sleep 3s

Type "wendy --device mac-mini.local:50051 device apps remove --force sh.wendy.examples.HelloMac"
Enter
Sleep 3s

Type "wendy --device mac-mini.local:50051 device apps list"
Enter
Sleep 5s
Type "q"
Sleep 500ms

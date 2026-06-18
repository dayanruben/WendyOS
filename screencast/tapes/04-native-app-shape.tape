Output deck/public/videos/mac-beta/04-native-app-shape.mp4
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false

Sleep 1s

Type "cd $(git rev-parse --show-toplevel)/Examples/HelloMac"
Enter
Sleep 500ms

Type "pwd"
Enter
Sleep 500ms

Type "cat wendy.json"
Enter
Sleep 2s

Type "ls Package.swift"
Enter
Sleep 1s

Type "ls Dockerfile docker-compose.yml compose.yml 2>/dev/null || echo 'No container manifests in this native Mac project.'"
Enter
Sleep 2s

Type "cd ../HelloXcode"
Enter
Sleep 500ms

Type "ls -d HelloXcode.xcodeproj"
Enter
Sleep 1s

Type "cat wendy.json"
Enter
Sleep 2s

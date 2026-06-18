Output deck/public/videos/mac-beta/08-unsupported.mp4
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

Type "wendy --device $WENDY_MAC_TARGET device hardware list"
Enter
Sleep 4s

Type "mkdir -p /tmp/wendy-mac-beta-container-demo && cd /tmp/wendy-mac-beta-container-demo"
Enter
Sleep 500ms

Type "cat > Dockerfile <<'EOF'"
Enter
Type "FROM alpine:3.20"
Enter
Type "CMD echo hello from a Linux container"
Enter
Type "EOF"
Enter
Sleep 500ms

Type "wendy run --device $WENDY_MAC_TARGET"
Enter
Sleep 5s

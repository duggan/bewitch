# Homepage highlight reel. Record with `make demo-video`, which starts a
# fresh mock daemon (scripts/demo-video/bewitch.toml) whose "incident"
# scenario plays a backup that saturates the CPU, heats the package and
# fills the root disk, tripping two alerts.
#
# Timeline (seconds after the shot opens; the incident starts at ~6):
#   0  dashboard, host at rest
#   6  incident begins: CPU, disk and network climb on the dashboard
#  12  CPU: all eight cores saturate
#  16  Procs: restic at the top, sorted by CPU
#  21  Hardware: package temperature rises through 85°C
#  27  Disk: / passes 90%
#  31  Alerts: both rules have fired; acknowledge one

Output site/demo-video/out/raw.mp4

# High-quality master; make demo-video encodes the web versions from it.
Set VideoCRF 8
Set VideoPreset slow
Set VideoBitrate 8000000

Set Shell bash
Set Width 1920
Set Height 1200
Set FontSize 22
Set Padding 24
Set Framerate 30
Set TypingSpeed 0
Set CursorBlink false
Set Theme { "name": "bewitch", "background": "#1A1A2E", "foreground": "#F8F8F2", "cursor": "#1A1A2E", "selection": "#BB86FC40", "black": "#1A1A2E", "red": "#FF5555", "green": "#50FA7B", "yellow": "#F1FA8C", "blue": "#BD93F9", "magenta": "#FF79C6", "cyan": "#8BE9FD", "white": "#F8F8F2", "brightBlack": "#6272A4", "brightRed": "#FF6E6E", "brightGreen": "#69FF94", "brightYellow": "#FFFFA5", "brightBlue": "#D6ACFF", "brightMagenta": "#FF92DF", "brightCyan": "#A4FFFF", "brightWhite": "#FFFFFF" }

Hide
Type "scripts/demo-video/wait-for-incident.sh /tmp/bewitch-demo/daemon.log 7 && clear && bin/bewitch -config scripts/demo-video/bewitch.toml"
Enter
Wait+Screen@120s /Dashboard/
Sleep 1s
Show

# At rest, then the backup kicks in.
Sleep 12s

Type "2"
Sleep 4s

Type "7"
Sleep 5s

Type "6"
Sleep 6s

Type "4"
Sleep 4s

Type "8"
Sleep 2.5s
Tab
Sleep 1.5s
Enter
Sleep 3s

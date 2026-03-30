# /record-demo - Record VHS Demo Video

Record a terminal demo video of bewitch using [vhs](https://github.com/charmbracelet/vhs) and update the site.

## Usage

```
/record-demo
```

## What This Skill Does

1. **Reviews and updates** the VHS tape script at `site/demo.tape`
2. **Starts `bewitchd` in mock mode** to generate synthetic data
3. **Records** the demo using `vhs site/demo.tape`
4. **Copies** the output to `site/public/demo.mp4`
5. **Cleans up** the daemon and temp files

## Prerequisites

- `vhs` must be installed (`brew install vhs`)
- `bewitch` and `bewitchd` must be built (`make build`)

## Mock Daemon Setup

The demo uses `data/bewitch.toml` which has `mock = true` — the daemon generates synthetic data without real collectors. Start it before recording and allow ~10 seconds for data to accumulate so history charts have content:

```bash
./bin/bewitchd -config data/bewitch.toml &>/tmp/bewitchd.log &
sleep 10
```

The mock config uses `/tmp/bewitch.sock` and `/tmp/bewitch.duckdb`, so it won't conflict with a real installation.

Kill the daemon after recording: `kill $(pgrep bewitchd)`

## VHS Tape Format

The tape script lives at `site/demo.tape`. Key VHS commands:

```
Output demo.mp4              # Output file (relative to CWD)
Set Width 1600               # Terminal width in pixels
Set Height 1200              # Terminal height in pixels
Set Shell zsh                # Shell to use
Set FontSize 16              # Font size
Set Theme "Catppuccin Mocha" # Terminal theme

Type "command"               # Type text
Enter                        # Press enter
Sleep 1s                     # Wait
Tab / Right / Left / Escape  # Key presses
PageUp / PageDown            # Scrolling
```

Full reference: https://github.com/charmbracelet/vhs

## View Order and Navigation

Views are defined in `internal/tui/app.go` as the `view` iota. The tab order is:

1. Dashboard
2. CPU
3. Memory
4. Disk
5. Network
6. Temperature (hidden if no data — visible in mock mode)
7. Power (hidden if no data — visible in mock mode)
8. Process
9. Alerts

Navigate with `Right`/`Left` arrow keys, `Tab`/`Shift+Tab`, or number keys `1`-`N`. Mock mode generates synthetic data for all collectors including temperature and power, so all 9 tabs are visible.

## Current Demo Flow

The tape walks through:
- Launching the TUI (`./bin/bewitch -config data/bewitch.toml`)
- Cycling through views with Right arrow (dashboard → cpu → memory → disk → network → process → alerts)
- Scrolling the process view with PageDown
- Navigating back with Tab
- Opening the alert creation form (`n`) and dismissing it (`Escape`)
- Quitting (`q`)

## Instructions

When invoked:

1. Read `site/demo.tape` to understand the current script
2. Review the current TUI views to see if the tape needs updating:
   - Check `internal/tui/app.go` for the current view order (the `view` iota and `updateVisibleTabs()`)
   - Ensure the tape exercises all visible views
   - On macOS, temperature and power views are hidden (no hardware), so the tape should account for that
3. Ask the user if they want any changes to the demo flow
4. Update `site/demo.tape` if needed
5. Start the mock daemon and wait for data:
   ```bash
   ./bin/bewitchd -config data/bewitch.toml &>/tmp/bewitchd.log &
   sleep 10
   ```
6. Run the recording:
   ```bash
   vhs site/demo.tape
   ```
7. Copy output and clean up:
   ```bash
   cp demo.mp4 site/public/demo.mp4
   rm demo.mp4
   kill $(pgrep bewitchd)
   ```
8. Ask if the user wants to rebuild the site (`cd site && bun run build`)

## Video Quality Settings

The tape includes video quality settings for the forked VHS build:

```
Set VideoCRF 18          # lower = higher quality (default: 20 MP4, 30 WebM)
Set VideoCodec libx264   # x264 for universal browser support (x265/HEVC lacks Chrome/Firefox support)
Set VideoPreset slow     # slower encode = better quality
Set PixelFormat yuv444p  # no chroma subsampling — critical for sharp colored bars
Set VideoBitrate 2000000 # 2Mbps (use raw number, not "2M" — VHS parser rejects suffix)
```

`yuv444p` is the key setting — default `yuv420p` destroys the pink/purple gradient bars in the TUI.

## Reviewing Recordings

Claude cannot view .mp4 files directly. Use ffmpeg to extract frames for review:

```bash
mkdir -p /tmp/demo-frames
ffmpeg -i site/public/demo.mp4 -vf fps=1 /tmp/demo-frames/frame_%02d.png
```

Then read individual PNGs to check quality.

## Notes

- VHS output goes to CWD (project root), not the tape's directory — the `Output` directive in the tape is just `demo.mp4`
- If views have been added/removed/reordered, count the `Right` key presses in the tape to ensure each view gets screen time
- Increase `Sleep` durations if views need more time to load data on first display

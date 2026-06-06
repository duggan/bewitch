#!/usr/bin/env python3
"""Generate site/static/og.png — the 1200x630 social/Open Graph card.

Builds an SVG (brand palette, glowing wordmark, tagline, the witch mascot
embedded as a data URI since librsvg won't load external files) and rasterises
it with rsvg-convert. Run via `make og`.
"""
import base64
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
STATIC = ROOT / "site" / "static"
WITCH = STATIC / "witch.png"
OUT = STATIC / "og.png"

witch_b64 = base64.b64encode(WITCH.read_bytes()).decode()

SVG = """<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="1200" height="630" viewBox="0 0 1200 630">
  <defs>
    <radialGradient id="glowTop" cx="42%" cy="-5%" r="75%">
      <stop offset="0%" stop-color="#7C4DFF" stop-opacity="0.28"/>
      <stop offset="55%" stop-color="#7C4DFF" stop-opacity="0"/>
    </radialGradient>
    <radialGradient id="glowWitch" cx="50%" cy="45%" r="55%">
      <stop offset="0%" stop-color="#FF6EC7" stop-opacity="0.30"/>
      <stop offset="70%" stop-color="#FF6EC7" stop-opacity="0"/>
    </radialGradient>
    <filter id="pinkGlow" x="-60%" y="-60%" width="220%" height="220%">
      <feGaussianBlur stdDeviation="10" result="b"/>
      <feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
  </defs>
  <rect width="1200" height="630" fill="#0a0a12"/>
  <rect width="1200" height="630" fill="url(#glowTop)"/>
  <ellipse cx="935" cy="320" rx="270" ry="270" fill="url(#glowWitch)"/>
  <image xlink:href="data:image/png;base64,@WITCH_B64@" x="795" y="150" width="300" height="319" preserveAspectRatio="xMidYMid meet"/>
  <text x="80" y="300" font-family="monospace" font-weight="bold" font-size="150" fill="#FF6EC7" filter="url(#pinkGlow)">bewitch</text>
  <rect x="86" y="330" width="300" height="6" rx="3" fill="#FF6EC7" opacity="0.85"/>
  <text x="88" y="410" font-family="sans-serif" font-size="40" fill="#BB86FC">A system monitor for the machines</text>
  <text x="88" y="462" font-family="sans-serif" font-size="40" fill="#BB86FC">nobody else is watching.</text>
  <text x="88" y="556" font-family="monospace" font-size="28" fill="#9E8CBA">bewitch.dev</text>
  <text x="300" y="556" font-family="monospace" font-size="28" fill="#FF6EC7">· in hot pink</text>
</svg>""".replace("@WITCH_B64@", witch_b64)

svg_path = ROOT / "site" / "og.svg.tmp"
svg_path.write_text(SVG)
try:
    subprocess.run(
        ["rsvg-convert", "-w", "1200", "-h", "630", str(svg_path), "-o", str(OUT)],
        check=True,
    )
finally:
    svg_path.unlink(missing_ok=True)

print(f"wrote {OUT.relative_to(ROOT)} ({OUT.stat().st_size} bytes)")

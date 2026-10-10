#!/bin/sh
# Encode the web version of the homepage reel and its poster frame from the
# raw VHS recording.
#
#   encode.sh <raw.mp4> <out dir>
#
# H.264 only: on this flat, text-heavy footage x264 (-tune animation) beats
# AV1 on size at equal sharpness, and it plays everywhere. CRF 24 is
# indistinguishable from the master at 100% crop.
set -eu
raw=$1
out=$2
mkdir -p "$out"

ffmpeg -v error -y -i "$raw" -an \
	-c:v libx264 -preset slow -tune animation -crf 24 -pix_fmt yuv420p \
	-movflags +faststart "$out/reel.mp4"

# Poster: the dashboard mid-incident (shown before playback and to visitors
# who prefer reduced motion). A 64-colour palette keeps the flat terminal
# colours and text crisp at a third of the size of a full-colour PNG.
ffmpeg -v error -y -ss 10.5 -i "$raw" -frames:v 1 \
	-vf "split[a][b];[a]palettegen=max_colors=64[p];[b][p]paletteuse=dither=none" \
	"$out/reel-poster.png"

ls -l "$out/reel.mp4" "$out/reel-poster.png"

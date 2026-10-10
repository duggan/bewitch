#!/bin/sh
# Block until LEAD seconds before the mock incident starts, so a recording
# begins on a calm host and the incident plays out on cue.
#
#   wait-for-incident.sh <daemon log> <lead seconds>
#
# The daemon logs "mock scenario starts at HH:MM:SS" (local time) once
# history seeding has finished.
set -eu
log=$1
lead=$2

tries=0
until line=$(grep -m1 'mock scenario starts at' "$log" 2>/dev/null); do
	tries=$((tries + 1))
	if [ "$tries" -gt 240 ]; then
		echo "wait-for-incident: no scenario start in $log after 120s" >&2
		exit 1
	fi
	sleep 0.5
done

start=$(echo "$line" | sed -E 's/.*starts at ([0-9:]+).*/\1/')
secs() { echo "$1" | awk -F: '{ print $1 * 3600 + $2 * 60 + $3 }'; }
wait=$(( $(secs "$start") - lead - $(secs "$(date +%H:%M:%S)") ))
if [ "$wait" -lt -43200 ]; then # the start is just past midnight
	wait=$((wait + 86400))
fi
if [ "$wait" -gt 0 ]; then
	sleep "$wait"
fi

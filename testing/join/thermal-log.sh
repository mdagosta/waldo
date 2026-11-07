#!/bin/sh
# Logs what WALDO_GPU_THROTTLE does to heat, from macmon (no sudo): each minute, the GPU's and CPU's
# busy share, GPU, chip and whole-system watts and GPU temperature averaged, and the fastest fan.
#   thermal-log.sh [log file]    (default ~/.waldo/join/logs/thermal.log)
set -eu
log=${1:-$HOME/.waldo/join/logs/thermal.log}
mkdir -p "$(dirname "$log")"
macmon pipe -i 1000 | python3 -u -c '
import json, sys, time
every = 60
print("time   gpu%  cpu%  gpuW  chipW  sysW  gpuC  fanRPM")
rows, fan = [], 0
for line in sys.stdin:
    s = json.loads(line)
    rows.append((s["gpu_active_ratio"] * 100, s["cpu_usage_pct"] * 100, s["gpu_power"], s["all_power"], s["sys_power"], s["temp"]["gpu_temp_avg"]))
    fan = max([fan] + [each["rpm"] for each in s.get("fans", [])])
    if len(rows) == every:
        average = [sum(column) / every for column in zip(*rows)]
        print(time.strftime("%H:%M"), "%5.0f %5.0f %5.1f %6.1f %5.1f %5.0f %7.0f" % (*average, fan))
        rows, fan = [], 0
' | tee -a "$log"

#!/bin/sh
# Types a command into this space's shell tab, the one that is not an agent's, and presses Enter.
#   launch-in-shell.sh <command...>      (run from a Claude tab: needs HC_TERMINAL_ID)
set -eu
[ -n "${HC_TERMINAL_ID:-}" ] || { echo "not in a Herdr terminal" >&2; exit 1; }
[ $# -gt 0 ] || { echo "usage: launch-in-shell.sh <command...>" >&2; exit 1; }
target=$(herdr-app ls | python3 -c "
import json, sys
me = '$HC_TERMINAL_ID'
for space in json.load(sys.stdin)['spaces']:
    terminals = [(tab, x) for tab in space['tabs'] for x in tab['terminals']]
    if any(x['terminal'] == me for _, x in terminals):
        shells = [x['terminal'] for tab, x in terminals if x['terminal'] != me and x.get('status') is None]
        named = [x['terminal'] for tab, x in terminals if tab.get('name') == 'shell' and x['terminal'] in shells]
        print((named or shells or [''])[0])
")
[ -n "$target" ] || { echo "no shell tab in this space" >&2; exit 1; }
hc send "$target" "$*"
echo "sent to $target"

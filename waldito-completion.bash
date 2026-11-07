# Bash completion for waldito: its commands, and the plans published in the runs repository, the
# ones `waldito <plan>` can launch. Add to ~/.bashrc:
#   source ~/src/openwaldo/waldo-waldito/waldito-completion.bash
_waldito() {
  local word=${COMP_WORDS[COMP_CWORD]} env runs commands
  [ "$COMP_CWORD" -eq 1 ] || { [ "${COMP_WORDS[1]}" = launch ] && [ "$COMP_CWORD" -eq 2 ]; } || return 0
  env=$HOME/.waldo/join/launch.env
  runs=${RUNS_REPO:-$(sed -n 's/.*RUNS_REPO:=\([^}]*\)}.*/\1/p' "$env" 2>/dev/null)}
  runs=${runs//\$HOME/$HOME}
  plans=$(git --git-dir="$runs" ls-tree --name-only HEAD runs/ 2>/dev/null | sed 's#^runs/##')
  commands=""
  [ "$COMP_CWORD" -eq 1 ] && commands="merge ingest check build join data check-plan launch"
  COMPREPLY=($(compgen -W "$commands $plans" -- "$word"))
}
complete -F _waldito waldito ./waldito

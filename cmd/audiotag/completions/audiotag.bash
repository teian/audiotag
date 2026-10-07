_audiotag() {
 local cur="${COMP_WORDS[COMP_CWORD]}" prev="${COMP_WORDS[COMP_CWORD-1]}"
 local commands="inspect set remove export import audiobook chapters cuesheet lyrics cover formats doctor tag-keys completion version"
 local flags="--help --recursive --dry-run --sync --backup --verify-audio --set --set-file --tags --remove --cover --chapters --config --part-from --transcript --chapter-range --json --output --raw --progress --no-progress"
 local context="$prev" prefix="" fragment="$cur" key suggestion logical
 COMPREPLY=()
 # bash-completion handles '=' word breaks when installed. Otherwise handle
 # the split KEY = VALUE tokens supplied by Bash itself.
 if declare -F _get_comp_words_by_ref >/dev/null; then
  local -a words
  local cword
  _get_comp_words_by_ref -n = cur prev words cword
  context="$prev"; fragment="$cur"
 else
  local start=$COMP_CWORD i
  while (( start > 1 )); do
   if [[ "${COMP_WORDS[start]}" == '=' || "${COMP_WORDS[start-1]}" == '=' ]]; then
    ((start--))
   else break; fi
  done
  if (( start < COMP_CWORD )); then
   fragment=""
   for ((i=start; i<=COMP_CWORD; i++)); do fragment+="${COMP_WORDS[i]}"; done
   context="${COMP_WORDS[start-1]}"
  fi
 fi
 if (( COMP_CWORD == 1 )); then
  COMPREPLY=( $(compgen -W "$commands" -- "$cur") ); return
 fi
 if [[ "${COMP_WORDS[1]}" == completion ]]; then
  COMPREPLY=( $(compgen -W 'bash zsh fish' -- "$cur") ); return
 fi
 logical="$fragment"
 # Also accept --option=value, without changing which part Readline replaces.
 case "$fragment" in
  --set=*|--set-file=*|--remove=*|--part-from=*|--progress=*|--tags=*|--config=*|--cover=*|--chapters=*|--transcript=*|--output=*)
   context="${fragment%%=*}"; prefix="$context="; fragment="${fragment#*=}";;
 esac
 case "$context" in
  --part-from) COMPREPLY=( $(compgen -W 'auto folder title filename' -- "$fragment") );;
  --progress) COMPREPLY=( $(compgen -W 'auto always never' -- "$fragment") );;
  --tags|--config|--cover|--chapters|--transcript|--output)
   while IFS= read -r suggestion; do COMPREPLY+=("$prefix$suggestion"); done < <(compgen -f -- "$fragment")
   _audiotag_trim_equals "$logical"
   return;;
  --set|--set-file|--remove)
   if [[ "$fragment" == *=* ]]; then
    [[ "$context" == --set-file ]] || return
    prefix="$prefix${fragment%%=*}="; fragment="${fragment#*=}"
    while IFS= read -r suggestion; do COMPREPLY+=("$prefix$suggestion"); done < <(compgen -f -- "$fragment")
   else
    while IFS= read -r key; do
     [[ "$key" == "$fragment"* ]] || continue
     [[ "$context" == --remove ]] || key="$key="
     COMPREPLY+=("$prefix$key")
    done < <(audiotag tag-keys 2>/dev/null)
    if [[ "$context" != --remove ]] && type compopt >/dev/null 2>&1; then compopt -o nospace 2>/dev/null; fi
   fi
   _audiotag_trim_equals "$logical"
   return;;
  *)
   if [[ "$cur" == -* ]]; then COMPREPLY=( $(compgen -W "$flags" -- "$cur") ); return; fi
   while IFS= read -r suggestion; do COMPREPLY+=("$suggestion"); done < <(compgen -f -- "$cur")
   return;;
 esac
 if [[ -n "$prefix" ]]; then
  local i
  for i in "${!COMPREPLY[@]}"; do COMPREPLY[$i]="$prefix${COMPREPLY[$i]}"; done
 fi
 _audiotag_trim_equals "$logical"
}
_audiotag_trim_equals() {
 # With = in Readline word breaks, only the suffix after the last = is replaced.
 if [[ "$COMP_WORDBREAKS" == *=* && "$1" == *=* ]]; then
  local trim="${1%=*}=" i
  for i in "${!COMPREPLY[@]}"; do COMPREPLY[$i]="${COMPREPLY[$i]#"$trim"}"; done
 fi
}
complete -o filenames -F _audiotag audiotag

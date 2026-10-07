#compdef audiotag
_audiotag_tag_assignment() {
 local mode=$1 current="${words[CURRENT]}"
 case "$current" in
  --set-file=*) compset -P '--set-file='; current="${current#*=}";;
  --set=*) compset -P '--set='; current="${current#*=}";;
 esac
 if [[ "$current" == *=* ]]; then
  if [[ "$mode" == file ]]; then
   # _files completes only the value, retaining the KEY= prefix.
   compset -P '*='
   _files
  fi
  return
 fi
 local -a keys
 keys=("${(@f)$(audiotag tag-keys 2>/dev/null)}")
 compadd -S '=' -- "${keys[@]}"
}
_audiotag_tag_remove() {
 local -a keys
 keys=("${(@f)$(audiotag tag-keys 2>/dev/null)}")
 compadd -- "${keys[@]}"
}
_audiotag() {
 local -a commands
 commands=('inspect:Inspect metadata' 'set:Set tags' 'remove:Remove tags' 'export:Export native metadata' 'import:Import native metadata' 'audiobook:Apply audiobook metadata' 'cuesheet:Export FLAC cuesheet' 'lyrics:Export synchronized lyrics' 'chapters:Read or write chapters' 'cover:Extract or replace cover' 'formats:List formats' 'doctor:Check dependencies' 'tag-keys:List common tag names' 'completion:Generate shell completion' 'version:Print version')
 if (( CURRENT == 2 )); then
  _describe 'command' commands
 elif [[ $words[2] == completion ]]; then
  _values 'shell' bash zsh fish
 else
  _arguments -s \
   '*--set[Set KEY=VALUE]:tag:_audiotag_tag_assignment text' \
   '*--set-file[Set KEY=FILE]:tag file:_audiotag_tag_assignment file' \
   '*--remove[Remove a tag]:tag:_audiotag_tag_remove' \
   '--tags[JSON tags]:file:_files' '--config[Book config]:file:_files' \
   '--cover[Cover image]:file:_files' '--chapters[Chapter JSON]:file:_files' \
   '--transcript[Markdown source]:file:_files' '--chapter-range[First-last]:range:' \
   '--part-from[Part authority]:source:(auto folder title filename)' \
   '--output[Output file]:file:_files' '--progress[Progress mode]:mode:(auto always never)' \
   '--recursive[Recurse directories]' '--dry-run[Preview]' '--sync[Synchronize explicitly edited ID3v1/ID3v2 fields]' \
   '--backup[Keep backup]' \
   '--verify-audio[Verify audio packets]' '--json[JSON output]' \
   '--raw[Native metadata JSON]' '--no-progress[Disable write progress]' \
   '--help[Show help]' '*:audio file:_files'
 fi
}
compdef _audiotag audiotag

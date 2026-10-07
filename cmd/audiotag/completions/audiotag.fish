function __audiotag_assignment --argument-names mode
 set -l current (commandline -ct)
 # Strip an attached option prefix, e.g. --set-file=lyrics=chapter.txt.
 set current (string replace -r '^--set(-file)?=' '' -- "$current")
 if string match -q '*=*' -- "$current"
  if test "$mode" = file
   set -l key (string split -m 1 '=' -- "$current")[1]
   set -l value (string split -m 1 '=' -- "$current")[2]
   for entry in (__fish_complete_path "$value")
    printf '%s=%s\n' "$key" "$entry"
   end
  end
 else
  for key in (audiotag tag-keys 2>/dev/null)
   printf '%s=\n' "$key"
  end
 end
end
complete -c audiotag -n '__fish_use_subcommand' -a 'inspect set remove export import audiobook chapters cuesheet lyrics cover formats doctor tag-keys completion version'
complete -c audiotag -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish' -f
for flag in recursive dry-run backup verify-audio json raw no-progress help
 complete -c audiotag -l $flag -f
end
complete -c audiotag -l set -x -a '(__audiotag_assignment text)'
complete -c audiotag -l set-file -x -a '(__audiotag_assignment file)'
complete -c audiotag -l remove -x -a '(audiotag tag-keys 2>/dev/null)'
for flag in tags config cover chapters transcript output
 complete -c audiotag -l $flag -r
end
complete -c audiotag -l part-from -xa 'auto folder title filename'
complete -c audiotag -l progress -xa 'auto always never'
complete -c audiotag -l chapter-range -r -f

complete -c audiotag -l sync -d 'Synchronize explicitly edited ID3v1/ID3v2 fields (MP3)'

## Demlo completion

[ -z "$XDG_CONFIG_HOME" ]; and set -l XDG_CONFIG_HOME $HOME/.config
[ -z "$XDG_DATA_DIRS" ]; and set -l XDG_DATA_DIRS /usr/local/share /usr/share

set -l user_script_cmd
set -l i "$XDG_CONFIG_HOME/demlo/scripts"
if [ -d "$i" ]; and [ -r "$i" ]
	set user_script_cmd "(ls -1 '$i')\t'User script'"
end

set -l system_script_cmd
for i in $XDG_DATA_DIRS/demlo/scripts
	if [ -d "$i" ]; and [ -r "$i" ]
		set system_script_cmd "(ls -1 '$i')\t'System script'"
		break
	end
end

set -l user_action_cmd
set -l i "$XDG_CONFIG_HOME/demlo/actions"
if [ -d "$i" ]; and [ -r "$i" ]
	set user_action_cmd "(ls -1 '$i')\t'User action'"
end

set -l system_action_cmd
for i in $XDG_DATA_DIRS/demlo/actions
	if [ -d "$i" ]; and [ -r "$i" ]
		set system_action_cmd "(ls -1 '$i')\t'System action'"
		break
	end
end

complete -c demlo -o c -d "Fetch cover"
complete -c demlo -o c=false -d "Do not fetch cover"
complete -c demlo -o color -d "Enable color output"
complete -c demlo -o color=false -d "Disable color output"
complete -c demlo -o cores -x -d "Number of cores" -a '(seq 0 (getconf _NPROCESSORS_ONLN))\tcores'
complete -c demlo -o debug -d "Enable debug output"
complete -c demlo -o debug=false -d "Disable debug output"
complete -c demlo -o exist -x -d "Add exist action" -a "$system_action_cmd $user_action_cmd"
complete -c demlo -o ext -x -d "Add search extension"
complete -c demlo -o i -r -d "Index"
complete -c demlo -o p -d "Process"
complete -c demlo -o p=false -d "Do not process"
complete -c demlo -o post -x -d "Postscript"
complete -c demlo -o pre -x -d "Prescript"
complete -c demlo -o r -x -d "Remove scripts"
complete -c demlo -o s -x -d "Add script" -a "$system_script_cmd $user_script_cmd"
complete -c demlo -o t -d "Fetch tags"
complete -c demlo -o t=false -d "Do not fetch tags"
complete -c demlo -o v -d "Print version"

#!/bin/bash

declare -A functions_list

functions_list=( 
    ["check_journalctl_log"]="$TESTSLIB/journalctl.sh" 
    ["get_journalctl_log"]="$TESTSLIB/journalctl.sh"
    ["is_core_system"]="$TESTSLIB/systems.sh"
    ["is_core18_system"]="$TESTSLIB/systems.sh"
    ["is_classic_system"]="$TESTSLIB/systems.sh"
    ["is_ubuntu_14_system"]="$TESTSLIB/systems.sh"    
    )

for function_name in "${!functions_list[@]}"; do
    lib_path="${functions_list[$function_name]}"
    file_name="$(echo "$function_name" | tr '_' '-')"

    params='"$@"'
    cat > "$TESTSLIB/bin/$file_name" <<-EOF
#!/bin/bash

# shellcheck source="$lib_path"
. "$lib_path"

$function_name $params
EOF
    chmod +x "$TESTSLIB/bin/$file_name"

done

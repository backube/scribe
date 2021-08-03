#! /bin/bash
# sleep forever
# sleep 9999999999

echo "Starting container"

set -e -o pipefail

echo "Scribe restic container version: ${version:-unknown}"
echo  "$@"


# Force the associated backup host name to be "scribe"
RESTIC_HOST="scribe"
# Make restic output progress reports every 10s
export RESTIC_PROGRESS_FPS=0.1

# Print an error message and exit
# error rc "message"
function error {
    echo "ERROR: $2"
    exit "$1"
}

# Error and exit if a variable isn't defined
# check_var_defined "MY_VAR"
function check_var_defined {
    if [[ -z ${!1} ]]; then
        error 1 "$1 must be defined"
    fi
}

function check_contents {
    echo "== Checking directory for content ==="
    DIR_CONTENTS="$(ls -A "${DATA_DIR}")"
    if [ -z "${DIR_CONTENTS}" ]; then
        echo "== Directory is empty skipping backup ==="
        exit 0
    fi
}

# Ensure the repo has been initialized
function ensure_initialized {
    echo "== Initialize Dir ======="
    # Try a restic command and capture the rc & output
    outfile=$(mktemp -q)
    if ! restic snapshots 2>"$outfile"; then
        output=$(<"$outfile")
        # Match against error string for uninitialized repo
        if [[ $output =~ .*(Is there a repository at the following location).* ]]; then
            restic init
        else
            error 3 "failure checking existence of repository"
        fi
    fi
    rm -f "$outfile"
}

function do_backup {
    echo "=== Starting backup ==="
    pushd "${DATA_DIR}"
    restic backup --host "${RESTIC_HOST}" .
    popd
}

function do_forget {
    echo "=== Starting forget ==="
    if [[ -n ${FORGET_OPTIONS} ]]; then
        #shellcheck disable=SC2086
        restic forget --host "${RESTIC_HOST}" ${FORGET_OPTIONS}
    fi
}

function do_prune {
    echo "=== Starting prune ==="
    restic prune
}

#######################################
# Trims the provided timestamp and
# returns one in the format: YYYY-MM-DD hh:mm:ss
# Globals:
#   None
# Arguments
#   Timestamp in format YYYY-MM-DD HH:mm:ss.ns
#######################################
function trim_timestamp() {
    local trimmed_timestamp=$(cut -d'.' -f1 <<<"$1")
    echo "${trimmed_timestamp}"
}

#######################################
# Provides the UNIX Epoch for the given timestamp
# Globals:
#   None
# Arguments:
#   timestamp
#######################################
function get_epoch_from_timestamp() {
    local timestamp="$1"
    local trimmed_timestamp=$(trim_timestamp "${timestamp}")
    local date_as_epoch=$(date --date="${trimmed_timestamp}" +%s)
    echo "${date_as_epoch}"
}


#######################################
# Reverses the elements within an array,
# inspired by: https://unix.stackexchange.com/a/412870
# Globals:
#   None
# Arguments:
#   Name of array
#######################################
function reverse_array() {
    local -n _arr=$1

    # set indices
    local -i left=0
    local -i right=$((${#_arr[@]} - 1))

    while ((${left} < ${right})); do
        # triangle swap
        local -i temp="${_arr[$left]}"
        _arr[$left]="${_arr[$right]}"
        _arr[$right]="$temp"

        # increment indices
        ((left++))
        ((right--))
    done
}

#######################################
# Selects the earliest Restic snapshot to restore from
# that was created before RESTORE_AS_OF. If SELECT_PREVIOUS
# is non-zero, then it selects the n-th snapshot older than RESTORE_AS_OF
# 
# RESTORE_AS_OF should be otherwise set to default from the Go client
# so this function would return whatever is the latest image at the time of creation
# Globals:
#   SELECT_PREVIOUS
#   RESTORE_AS_OF
# Arguments:
#   None
#######################################
function select_restic_snapshot_to_restore() {
    local offset=${SELECT_PREVIOUS}
    local target_epoch=$(get_epoch_from_timestamp "${RESTORE_AS_OF}")
    # list of epochs
    declare -a epochs
    # create an associative array that maps numeric epoch to the restic snapshot IDs
    declare -A epochs_to_snapshots

    # go through the timestamps received from restic
    IFS=$'\n'
    for line in $(restic -r ${RESTIC_REPOSITORY} snapshots | grep /data | awk '{print $1 "\t" $2 " " $3}'); do
        # extract the proper variables
        local snapshot_id=$(echo -e ${line} | cut -d$'\t' -f1)
        local snapshot_ts=$(echo -e ${line} | cut -d$'\t' -f2)
        local snapshot_epoch=$(get_epoch_from_timestamp ${snapshot_ts})
        epochs+=("${snapshot_epoch}")
        epochs_to_snapshots[${snapshot_epoch}]="${snapshot_id}"
    done

    # reverse the list so the first element has the most recent timestamp
    reverse_array epochs
    local -i idx=0
    # find the first epoch in the list less than or equal to RESTORE_AS_OF
    while ((${target_epoch} < ${epochs[${idx}]})); do
        local -i nextIdx=${idx}+1
        # if we reached the end of the list
        if ((${nextIdx} == ${#epochs[@]})); then
        break
        fi
        ((idx++))
    done

    # get the epoch + the offset, or the oldest epoch
    local -i lastIdx=$((${#epochs[@]} - 1))
    local -i offsetIdx=$((${idx} + ${offset}))
    local selectedEpoch=${epochs[${lastIdx}]}
    if ((${offsetIdx} <= ${lastIdx})); then
        selectedEpoch=${epochs[${offsetIdx}]}
    fi
    local selectedId=${epochs_to_snapshots[${selectedEpoch}]}
    echo "${selectedId}"
}


function do_restore {
    echo "=== Starting restore ==="
    echo "RestoreAsOf: ${RESTORE_AS_OF}"
    echo "SelectPrevious: ${SELECT_PREVIOUS}"
    local snapshot_id=$(select_restic_snapshot_to_restore)
    sleep 999999
    pushd "${DATA_DIR}"
    restic restore -t . --host "${RESTIC_HOST}" "${snapshot_id}"
    popd
}

echo "Testing mandatory env variables"
# Check the mandatory env variables
for var in RESTIC_CACHE_DIR \
           RESTIC_PASSWORD \
           RESTIC_REPOSITORY \
           DATA_DIR \
           RESTORE_AS_OF \
           SELECT_PREVIOUS \
           ; do
    check_var_defined $var
done

for op in "$@"; do
    case $op in
        "backup")
            check_contents
            ensure_initialized
            do_backup
            do_forget
            ;;
        "prune")
            do_prune
            ;;
        "restore")
            do_restore
            ;;
        *)
            error 2 "unknown operation: $op"
            ;;
    esac
done

echo "=== Done ==="

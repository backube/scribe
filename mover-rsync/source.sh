#! /bin/bash

set -e -o pipefail

echo "Scribe rsync container version: ${version:-unknown}"

# Ensure we have connection info for the destination
DESTINATION_PORT="${DESTINATION_PORT:-22}"
if [[ -z "$DESTINATION_ADDRESS" ]]; then
    echo "Remote host must be provided in DESTINATION_ADDRESS"
    exit 1
fi

mkdir -p ~/.ssh/controlmasters
chmod 711 ~/.ssh

# Provide ssh host key to validate remote
echo "$DESTINATION_ADDRESS $(</keys/destination.pub)" > ~/.ssh/known_hosts

cat - <<SSHCONFIG > ~/.ssh/config
Host *
  # Wait max 30s to establish connection
  ConnectTimeout 30
  # Control persist to speed 2nd ssh connection
  ControlMaster auto
  ControlPath ~/.ssh/controlmasters/%C
  ControlPersist 5
  # Disables warning when IP is added to known_hosts
  CheckHostIP no
  # Use the identity provided via attached Secret
  IdentityFile /keys/source
  Port ${DESTINATION_PORT}
  # Enable protocol-level keepalive to detect connection failure
  ServerAliveCountMax 4
  ServerAliveInterval 30
  # We know the key of the server, so be strict
  StrictHostKeyChecking yes
  # Using protocol-level, so we don't need TCP-level
  TCPKeepAlive no
SSHCONFIG

echo "Syncing data to ${DESTINATION_ADDRESS}:${DESTINATION_PORT} ..."
START_TIME=$SECONDS
if [ -z ${REVERSE_SYNC} ]; then
	rsync -aAhHSxz --delete --itemize-changes --info=stats2,misc2 "root@${DESTINATION_ADDRESS}":. /data/
	echo "Syncing data to ${DESTINATION_ADDRESS}:${DESTINATION_PORT} ..."
else
	rsync -aAhHSxz --delete --itemize-changes --info=stats2,misc2 /data/ "root@${DESTINATION_ADDRESS}":.
	echo "Syncing data from ${DESTINATION_ADDRESS}:${DESTINATION_PORT} ..."
fi
rc=$?
echo "Rsync completed in $(( SECONDS - START_TIME ))s"
if [[ $rc -eq 0 ]]; then
    echo "Synchronization completed successfully. Notifying destination..."
    ssh "root@${DESTINATION_ADDRESS}" shutdown 0
else
    echo "Synchronization failed. rsync returned: $rc"
    exit $rc
fi

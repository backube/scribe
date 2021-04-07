#!/bin/bash

POD_UID=$(cat /app/podinfo/uid)
VERIFY_DIR="/app/verify/$HOSTNAME"
DATA_DIR="/app/data/$HOSTNAME"
COUNTER_FILE="counter.yaml"
RANDOM_FILE="random.b64"
# RANDOM_FILE="random-COUNT.b64"
let COUNT=0

main() {
  prepare

  while true
  do
    pre_stop_hook

    prepare_commit
    cd $DATA_DIR && do_commit
    cd $VERIFY_DIR && do_commit

    if (( COUNT % 100 == 0 ))
    then
      cd $VERIFY_DIR && do_fsck
      cd $DATA_DIR && do_fsck
      read_counter_file

      if [ "$APP_TYPE" == "mini" ]
      then
        while true; do pre_stop_hook; sleep 1; done
        exit 1
      fi

    fi
  done
}

# graceful stop hook
pre_stop_hook() {
    if [ -f /pre-stop-start ]
    then
      touch /pre-stop-done
      exit 0
    fi
}

prepare() {
  mkdir -p $VERIFY_DIR
  mkdir -p $DATA_DIR

  # if the global .gitconfig is not writable we also try to write to the individual repos
  git config --global user.name "Livmig Test App"
  git config --global user.email "testapp@livmig.io"
  cd $VERIFY_DIR
  git config user.name "Livmig Test App"
  git config user.email "testapp@livmig.io"
  cd $DATA_DIR
  git config user.name "Livmig Test App"
  git config user.email "testapp@livmig.io"

  cd $VERIFY_DIR
  if git rev-parse --is-inside-work-tree 2> /dev/null
  then

    cd $DATA_DIR
    if git rev-parse --is-inside-work-tree 2> /dev/null
    then
      cd $VERIFY_DIR && do_fsck
      cd $DATA_DIR && do_fsck
      read_counter_file
    else
      echo "ERROR: git not inited in $DATA_DIR but already inited in $VERIFY_DIR"
      sleep 10
      exit 1
    fi

  else

    cd $DATA_DIR
    if git rev-parse --is-inside-work-tree 2> /dev/null
    then
      echo "ERROR: git already inited in $DATA_DIR but not inited in $VERIFY_DIR"
      sleep 10
      exit 1
    else
      cd $VERIFY_DIR && do_init
      cd $DATA_DIR && do_init
    fi

  fi
}

read_counter_file() {

  # read current value to continue the count from  
  cd $DATA_DIR
  COUNT="$(head -1 $COUNTER_FILE | cut -d: -f2)"
  
  cd $VERIFY_DIR
  COUNT_VERIFY="$(head -1 $COUNTER_FILE | cut -d: -f2)"

  if (( COUNT == COUNT_VERIFY ))
  then
    echo "=== read counter $COUNT ==="
  elif (( COUNT == COUNT_VERIFY + 1 ))
  then
    # we might be off by one because of order of writes
    echo "=== read counter $COUNT (verify is off by one) ==="
  else
    echo "ERROR: counter mismatch COUNT=$COUNT vs. COUNT_VERIFY=$COUNT_VERIFY"
    exit 3
  fi
}

prepare_commit() {
  let COUNT++
  COUNTER_DATA="\
count: $COUNT
date: $(date)
HEAD: $(git rev-parse HEAD)
"
  RANDOM_DATA="$(node -p 'crypto.randomBytes(16).toString("base64")')"
}

do_commit() {
  echo "=== git commit $PWD count: $COUNT ==="
  printf "%s\n" "$COUNTER_DATA" > $COUNTER_FILE
  printf "%s\n" "$RANDOM_DATA" > ${RANDOM_FILE/COUNT/$COUNT}
  git add $COUNTER_FILE ${RANDOM_FILE/COUNT/$COUNT}
  git commit -a -m "count: $COUNT" || exit 3
}

do_init() {
  echo "=== git init $PWD ==="
  git init .
}

do_fsck() {
  echo "=== git fsck $PWD ==="
  git fsck --full --strict || exit 2
}

main

#!/bin/bash

SRC_SC="portworx-csi-sc"
DEST_SC="ocs-storagecluster-ceph-rbd"
STS="testapp-git"

main() {

  read_sts_info

  MANUAL="first"
  start_replications
  wait_replications

  echo
  echo "=== downtime start - $(date) ==="
  echo

  quiesce

  MANUAL="final"
  trigger_replications
  wait_replications

  replace_volumes
  stop_replications

  unquiesce

  echo
  echo "=== downtime end - $(date) ==="
  echo

  echo
  echo "=== livmig: done ==="
  echo
}

start_replications() {
  echo
  echo "=== livmig: start replications ($MANUAL) ==="
  echo
  for PVC in $PVCS
  do
    echo "PVC: $PVC"
    read_pvc_info
    echo "    - PV: $PV"
    echo "    - CAPACITY: $CAPACITY"
    echo "    - creating replication destination ..."
    create_dest
    wait_dest_address
    echo "    - DEST_ADDRESS: $DEST_ADDRESS"
    echo "    - creating replication source ..."
    create_src
    echo "    - ready"
  done
}

wait_replications() {
  echo
  echo "=== livmig: wait replications ($MANUAL) ==="
  echo
  for PVC in $PVCS
  do
    echo "PVC: $PVC"
    SRC_MANAUL_SYNC=""
    DEST_MANAUL_SYNC=""
    while [ "$SRC_MANAUL_SYNC" != "$MANUAL" ] || [ "$DEST_MANAUL_SYNC" != "$MANUAL" ]
    do
      sleep 1
      SRC_MANAUL_SYNC=$(kubectl get replicationsource $PVC --template={{.status.lastManualSync}})
      DEST_MANAUL_SYNC=$(kubectl get replicationdestination $PVC --template={{.status.lastManualSync}})
      echo "    - SRC_MANAUL_SYNC: $SRC_MANAUL_SYNC"
      echo "    - DEST_MANAUL_SYNC: $DEST_MANAUL_SYNC"
    done
  done
}

trigger_replications() {
  echo
  echo "=== livmig: mark replications ($MANUAL) ==="
  echo
  for PVC in $PVCS
  do
    # using pause because delete will also delete the dest snapshot/volume
    kubectl patch replicationsources $PVC --type merge -p '{"spec":{"trigger":{"manual":"'$MANUAL'"}}}'
    kubectl patch replicationdestinations $PVC --type merge -p '{"spec":{"trigger":{"manual":"'$MANUAL'"}}}'
  done
}

stop_replications() {
  echo
  echo "=== livmig: stop replications ==="
  echo
  for PVC in $PVCS
  do
    kubectl delete replicationsources $PVC
    kubectl delete replicationdestinations $PVC
  done
}


quiesce() {
  echo
  echo "=== livmig: quiesce ==="
  echo
  kubectl scale --current-replicas=$STS_REPLICAS --replicas=0 sts $STS
  kubectl delete pod -l app=$STS --now
}

unquiesce() {
  echo
  echo "=== livmig: unquiesce ==="
  echo
  kubectl scale --current-replicas=0 --replicas=$STS_REPLICAS sts $STS
}


replace_volumes() {
  echo
  echo "=== livmig: replace volumes ==="
  echo
  for PVC in $PVCS
  do
    echo "PVC: $PVC"
    read_pvc_info
    DEST_SOURCE_API_GROUP=$(kubectl get replicationdestination $PVC --template={{.status.latestImage.apiGroup}})
    DEST_SOURCE_KIND=$(kubectl get replicationdestination $PVC --template={{.status.latestImage.kind}})
    DEST_SOURCE_NAME=$(kubectl get replicationdestination $PVC --template={{.status.latestImage.name}})
    echo "    - PV: $PV"
    echo "    - CAPACITY: $CAPACITY"
    echo "    - DEST_SOURCE_API_GROUP: $DEST_SOURCE_API_GROUP"
    echo "    - DEST_SOURCE_KIND: $DEST_SOURCE_KIND"
    echo "    - DEST_SOURCE_NAME: $DEST_SOURCE_NAME"
    # echo "    - check volumes ..."
    # echo "      - count value on volumes: "
    # minikube ssh -- sudo find /var/lib/kubelet/pods -name testapp.yaml -exec 'head -1 {} \;' -printf ' %p\\n'
    # echo "      - TODO !!! "
    # TODO ...
    echo "    - deleting old pvc ..."
    # kubectl patch pv $PV -p '{"spec": {"persistentVolumeReclaimPolicy": "Retain"}}'
    kubectl delete pvc $PVC
    echo "    - creating pvc from dest ..."
    create_pvc_from_dest
    wait_pvc_ready
    echo "    - ready"
  done
}

wait_dest_address() {
  DEST_ADDRESS=""
  while [ -z "$DEST_ADDRESS" ] || [ "$DEST_ADDRESS" == "<no value>" ]
  do
    [ -z "$DEST_ADDRESS" ] || sleep 3
    DEST_ADDRESS=$(kubectl get replicationdestination $PVC --template={{.status.rsync.address}})
  done
}

wait_pvc_ready() {
  PVC_PHASE=""
  while [ "$PVC_PHASE" != "Bound" ]
  do
    [ -z "$PVC_PHASE" ] || sleep 3
    PVC_PHASE=$(kubectl get pvc $PVC --template={{.status.phase}})
  done
}

read_sts_info() {
  STS_REPLICAS=$(kubectl get sts $STS --template={{.spec.replicas}})
  STS_PVC_TEMPLATE_NAMES=$(kubectl get sts $STS --template='{{range .spec.volumeClaimTemplates}}{{if eq .spec.storageClassName "'$SRC_SC'"}}{{.metadata.name}} {{end}}{{end}}')
  STS_PVC_NAME_PATTERN="^($(join_by '|' $STS_PVC_TEMPLATE_NAMES))-$STS-(0|[1-9][0-9]*)$"
  PVCS=$(kubectl get pvc -o name | cut -d/ -f2 | egrep --color=never "$STS_PVC_NAME_PATTERN")
}

read_pvc_info() {
  CAPACITY=$(kubectl get pvc $PVC --template={{.status.capacity.storage}})
  PV=$(kubectl get pvc $PVC --template={{.spec.volumeName}})
}

create_dest() {
  kubectl create -f - <<EOF
apiVersion: scribe.backube/v1alpha1
kind: ReplicationDestination
metadata:
  name: $PVC
spec:
  trigger:
    manual: $MANUAL
  rsync:
    copyMethod: None
    capacity: $CAPACITY
    storageClassName: $DEST_SC
    accessModes: [ReadWriteOnce]
    serviceType: ClusterIP
EOF
}

create_src() {
  kubectl create -f - <<EOF
apiVersion: scribe.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: $PVC
spec:
  sourcePVC: $PVC
  trigger:
    manual: $MANUAL
  rsync:
    copyMethod: Clone
    address: $DEST_ADDRESS
    sshKeys: scribe-rsync-dest-src-$PVC
    storageClassName: $SRC_SC
EOF
}

create_pvc_from_dest() {
    kubectl create -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $PVC
spec:
  storageClassName: $DEST_SC
  dataSource:
    apiGroup: $DEST_SOURCE_API_GROUP
    kind: $DEST_SOURCE_KIND
    name: $DEST_SOURCE_NAME
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: $CAPACITY
EOF
}

join_by() { local IFS="$1"; shift; echo "$*"; }

main

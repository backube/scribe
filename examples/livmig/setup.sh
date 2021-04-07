#!/bin/bash

[[ " $* " =~ " --dashboard " ]] && USE_DASHBOARD=true

if minikube status
then
  echo
  echo "WARNING: minikube already exists"
  echo "         (you can use --delete to setup from fresh)"
  echo
else
  echo
  echo "=== setup: start minikube ==="
  echo

  minikube config set driver hyperkit
  minikube config set memory 8192
  minikube config set cpus 6
  minikube config view
  minikube start
fi

echo
echo "=== setup: enable dashboard (DISABLED - TO ENABLE EDIT setup.sh)..."
echo

if [ "$USE_DASHBOARD" == "true" ]
then
  # setup dashboard services
  minikube addons enable metrics-server
  minikube addons enable dashboard
  # change service from ClusterIP to NodePort to make it accessible for the browser
  kubectl patch -n kubernetes-dashboard service kubernetes-dashboard -p '{ "spec": { "type": "NodePort" }}'
fi


echo
echo "=== setup: enable storage services ..."
echo

# https://minikube.sigs.k8s.io/docs/tutorials/volume_snapshots_and_csi/
minikube addons enable volumesnapshots
minikube addons enable csi-hostpath-driver

echo
echo "=== setup: create VolumeSnapshotClass"
echo

# setup a VolumeSnapshotClass
kubectl create -f - <<EOF
apiVersion: snapshot.storage.k8s.io/v1beta1
kind: VolumeSnapshotClass
metadata:
  name: csi-hostpath-snapclass
driver: hostpath.csi.k8s.io
deletionPolicy: Delete
EOF

echo
echo "=== setup: set default classes ==="
echo

# setup csi-hostpath-snapclass as default VolumeSnapshotClass
kubectl annotate volumesnapshotclass/csi-hostpath-snapclass snapshot.storage.kubernetes.io/is-default-class=true --overwrite

# setup csi-hostpath-sc as default storage class
kubectl annotate sc/standard storageclass.kubernetes.io/is-default-class=false --overwrite
kubectl annotate sc/csi-hostpath-sc storageclass.kubernetes.io/is-default-class=true --overwrite

if [ "$USE_DASHBOARD" == "true" ]
then
  echo
  echo "=== status: dashboard url ==="
  echo

  minikube service kubernetes-dashboard --url -n kubernetes-dashboard
fi

echo
echo "=== setup: done ==="
echo

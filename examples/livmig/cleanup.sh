#!/bin/bash

[[ " $* " =~ " --ns " ]] && DELETE_NS=true

if [ "$DELETE_NS" == "true" ]
then

  echo
  echo "=== cleanup: delete ns ==="
  echo

  kubectl delete ns livmig --now || echo "namespace livmig not found"

else

  echo
  echo "=== cleanup: testapp-git ..."
  echo

  kubectl delete -f testapp-git/app.yaml

  echo
  echo "=== cleanup: replications ..."
  echo

  kubectl delete --all replicationsources
  kubectl delete --all replicationdestinations

  echo
  echo "=== cleanup: PVCs and snapshots ..."
  echo

  kubectl delete --all pvc
  kubectl delete --all volumesnapshots

fi

echo
echo "=== cleanup: rm -rf /tmp/testapp_git_verify ..."
echo

minikube ssh -- sudo rm -rf /tmp/testapp_git_verify
minikube ssh -- sudo mkdir -p /tmp/testapp_git_verify

echo
echo "=== cleanup: done."
echo

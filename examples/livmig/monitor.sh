#!/bin/bash

[[ " $* " =~ " --du " ]] && SHOW_DU=true
[[ " $* " =~ " --minikube " ]] && SHOW_MINIKUBE=true

echo "=== PODS ==="
echo
kubectl get pod

echo
echo "=== PVCs ==="
echo
kubectl get pvc -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,STORAGE-CLASS:.spec.storageClassName

echo
echo "=== Replications ==="
echo
kubectl get replicationdestination
kubectl get replicationsources

echo
echo "=== LOG ==="
echo
kubectl logs testapp-git-0 --tail=10

if [ "$SHOW_MINIKUBE" == "true" ]
then

  echo
  echo "=== DATA ==="
  echo
  minikube ssh -- \
    sudo find /var/lib/kubelet/pods \
    -name counter.yaml \
    -exec 'head -1 {} \;' \
    -printf '\\n\\n' \
    || echo "???"
    # -printf '\\n%p\\n\\n' \

  echo "=== VERIFY ==="
  echo
  minikube ssh -- \
    sudo find /tmp/testapp_git_verify \
      -name counter.yaml \
      -exec 'head -1 {} \;' \
      -printf '\\n\\n' \
      || echo "???"
      # -printf '\\n%p\\n\\n' \

  if [ "$SHOW_DU" == "true" ]
  then
      echo
      echo "=== DISK USAGE ==="
      echo

      minikube ssh -- sudo du -sh /var/lib/kubelet/ || echo "???"
  fi
fi

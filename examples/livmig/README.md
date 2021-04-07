# Live Migration Example

This is a sample automation that shows how to migrate a live cluster between storage-classes and across storage-providers with minimal interruption to running workloads. The goal is to explain what can be done today, to identify enhancements opportunities, and plan upstream work.

## Setup

First we need to setup our cluster with CSI and volume-snapshot.
I used minikube and the csi-hostpath-driver + volumesnapshots addons.
To setup on minikube use the setup.sh script in this dir.

```
./setup.sh
```

## Scribe Operator

To build and run the operator locally from the repo:

```
./operator.sh # runs locally
```

Or deploy the operator pod from helm chart:

```
./operator.sh --helm
```

## Start Test Application

The test application is verifying the data in its PVC against another hostpath volume,
and uses git internally to check that the history of changes is also consistent.
See the app/ dir for the code and configs of the app, and use start.sh to deploy it.

```
./start.sh
```

## Monitor the application

In order to get a sense of what the application is doing use the monitor.sh script with watch:

```
watch ./monitor.sh
```

The output looks similar to the output below. The interesting information is the `count` values - this number will be verified between the DATA volume and the hostpath verify volume. When running the monitor you will typically see these counts differ because there is some delay between the time the script will sample each of these files, but as long is the application keeps running and does not fail, it means that they match. The application will keep updating the counts as it runs, and should keep updating it after a live migration too.

```
=== PODS ===

NAME        READY   STATUS    RESTARTS   AGE
testapp-0   1/1     Running   0          3m13s

=== PVCs ===

NAME             STATUS   STORAGE-CLASS
data-testapp-0   Bound    csi-hostpath-sc

=== Replications ===

No resources found in livmig namespace.
No resources found in livmig namespace.

=== DATA ===

count: 8232

=== VERIFY ===

count: 8234

```

## Run Live Migration

```
./livmig.sh
```

This is a sample output:

```
$ ./livmig.sh

=== livmig: start replications (first) ===

PVC: data-testapp-0
    - PV: pvc-7f0a6702-3ef8-4de9-8182-1126102267e9
    - CAPACITY: 1Gi
    - creating replication destination ...
replicationdestination.scribe.backube/data-testapp-0 created
    - DEST_ADDRESS: 10.98.179.73
    - creating replication source ...
replicationsource.scribe.backube/data-testapp-0 created
    - ready

=== livmig: wait replications (first) ===

PVC: data-testapp-0
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: <no value>
    - DEST_MANAUL_SYNC: <no value>
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first

=== downtime start - Wed Apr  7 19:57:43 IDT 2021 ===


=== livmig: quiesce ===

statefulset.apps/testapp scaled
pod "testapp-0" deleted

=== livmig: mark replications (final) ===

replicationsource.scribe.backube/data-testapp-0 patched
replicationdestination.scribe.backube/data-testapp-0 patched

=== livmig: wait replications (final) ===

PVC: data-testapp-0
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: first
    - DEST_MANAUL_SYNC: first
    - SRC_MANAUL_SYNC: final
    - DEST_MANAUL_SYNC: final

=== livmig: replace volumes ===

PVC: data-testapp-0
    - PV: pvc-7f0a6702-3ef8-4de9-8182-1126102267e9
    - CAPACITY: 1Gi
    - DEST_SOURCE_API_GROUP: snapshot.storage.k8s.io
    - DEST_SOURCE_KIND: VolumeSnapshot
    - DEST_SOURCE_NAME: scribe-dest-data-testapp-0-20210407195825
    - deleting old pvc ...
persistentvolumeclaim "data-testapp-0" deleted
    - creating pvc from dest ...
persistentvolumeclaim/data-testapp-0 created
    - ready

=== livmig: stop replications ===

replicationsource.scribe.backube "data-testapp-0" deleted
replicationdestination.scribe.backube "data-testapp-0" deleted

=== livmig: unquiesce ===

statefulset.apps/testapp scaled

=== downtime end - Wed Apr  7 19:59:29 IDT 2021 ===


=== livmig: done ===
```

## Cleanup

Use the cleanup script:

```
./cleanup.sh
```


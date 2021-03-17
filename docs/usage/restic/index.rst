========================
Restic-based replication
========================



.. sidebar:: Contents

   .. contents:: Restic-based replication

This document covers the design of the restic-based data mover.

.. contents::
   :depth: 2

Overview
========

Restic is backup program that can back up your files
from Linux, BSD, Mac and Windows to many different storage types, including self-hosted
and online services. Read more about `Restic <https://restic.net/>`_

Restic based data mover uses ``restic-config`` to read confgirations details for backup and restore.

ResticConfig
============

.. code::  yaml

   apiVersion: v1
   kind: Secret
   metadata:
   name: restic-config
   type: Opaque
   stringData:
      # The repository url
      RESTIC_REPOSITORY: s3:http://minio.minio.svc.cluster.local:9000/restic-repo
      # The repository encryption key
      RESTIC_PASSWORD: my-secure-restic-password
      # ENV vars specific to the back end
      # https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html
      AWS_ACCESS_KEY_ID: access_key_id
      AWS_SECRET_ACCESS_KEY: password

ReplicationSoucreResticSpec
===========================

.. code:: yaml
   
   ---
   apiVersion: scribe.backube/v1alpha1
   kind: ReplicationSource
   metadata:
   name: restic-source
   namespace: source
   spec:
      sourcePVC: data-vol
      trigger:
         schedule: "*/10 * * * *"
      restic:
         resticConfig: "restic-config"
         copyMethod: Snapshot
         resticArg: "backup"

Once the above CR is applied, the controller reconcile the spec and schedule
the next sync based on the ``spec.trigger.schedule``. Depending on ``spec.restic.copyMethod``,
the controller either initiate a snapshot/clone/none type of replication. ``spec.restic.resticArg``
specifies whether to do ``backup``, ``restore`` or ``prune``. Since we want to create a backup of
the source pvc ``data-vol`` we used ``spec.restic.resticArg: "backup"``.

ReplicationDestinationResticSpec
================================

.. code:: yaml

   ---
   apiVersion: scribe.backube/v1alpha1
   kind: ReplicationDestination
   metadata:
   name: destination
   namespace: dest
   spec:
   trigger:
      schedule: "*/5 * * * *"
   restic:
      resticConfig: "restic-secret"
      resticArg: "restore"
      copyMethod: Snapshot
      accessModes: [ReadWriteOnce]
      capacity: 2Gi

On the destination side the controller restores the the backup by mirroring into a temproray pvc.
Since ``spec.restic.copyMethod`` is ``Snapshot``, the controller creates a snapshot of the
temproray pvc and save the snapshot name in ``.status.latestImage.name`` of the destination cr.
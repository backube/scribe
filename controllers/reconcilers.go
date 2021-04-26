/*
Copyright 2021 The Scribe authors.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func createOrUpdateJobRsync(ctx context.Context,
	l logr.Logger,
	c client.Client,
	job *batchv1.Job,
	owner metav1.Object,
	scheme *runtime.Scheme,
	labels map[string]string,
	envVars []corev1.EnvVar,
	command []string,
	dataPVCName string,
	sshSecretName string,
	paused bool,
	saName string) (bool, error) {
	runAsUser := int64(0)
	containers := []corev1.Container{{
		Name:    "rsync",
		Env:     envVars,
		Command: command,
		Image:   RsyncContainerImage,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"AUDIT_WRITE",
					"SYS_CHROOT",
				},
			},
			RunAsUser: &runAsUser,
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: mountPath},
			{Name: "keys", MountPath: "/keys"},
		},
	}}
	secretMode := int32(0600)
	volumes := []corev1.Volume{
		{Name: dataVolumeName, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: dataPVCName,
			}},
		},
		{Name: "keys", VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  sshSecretName,
				DefaultMode: &secretMode,
			}},
		},
	}

	return createOrUpdateJob(ctx, l, c, job, owner,
		scheme, labels, containers, volumes, paused, saName)
}

func createOrUpdateJob(ctx context.Context,
	l logr.Logger,
	c client.Client,
	job *batchv1.Job,
	owner metav1.Object,
	scheme *runtime.Scheme,
	labels map[string]string,
	containers []corev1.Container,
	volumes []corev1.Volume,
	paused bool,
	saName string) (bool, error) {
	backoffLimit := int32(2)

	op, err := ctrlutil.CreateOrUpdate(ctx, c, job, func() error {
		if err := ctrl.SetControllerReference(owner, job, scheme); err != nil {
			l.Error(err, "unable to set controller reference")
			return err
		}
		if job.Spec.Template.Labels == nil {
			job.Spec.Template.Labels = map[string]string{}
		}
		for k, v := range labels {
			job.Spec.Template.Labels[k] = v
		}
		job.Spec.BackoffLimit = &backoffLimit
		parallelism := int32(1)
		if paused {
			parallelism = 0
		}
		job.Spec.Parallelism = &parallelism
		job.Spec.Template.Spec.Containers = containers
		job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		job.Spec.Template.Spec.ServiceAccountName = saName
		job.Spec.Template.Spec.Volumes = volumes
		return nil
	})
	if job.Status.Failed >= backoffLimit {
		l.Info("deleting job -- backoff limit exceeded")
		err = c.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
		return false, err
	}
	if err != nil {
		l.Error(err, "job reconcile failed")
		return false, err
	}

	l.Info("job reconciled", "operation", op)
	return true, nil
}

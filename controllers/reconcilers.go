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

func createOrUpdateJobRclone(ctx context.Context,
	l logr.Logger,
	c client.Client,
	job *batchv1.Job,
	owner metav1.Object,
	scheme *runtime.Scheme,
	dataPVCName string,
	rcloneSecretName string,
	destPath string,
	direction string,
	configSection string,
	paused bool,
	saName string) (bool, error) {
	env := []corev1.EnvVar{
		{Name: "RCLONE_DEST_PATH", Value: destPath},
		{Name: "DIRECTION", Value: direction},
		{Name: "RCLONE_CONFIG", Value: "/rclone-config/rclone.conf"},
		{Name: "RCLONE_CONFIG_SECTION", Value: configSection},
		{Name: "MOUNT_PATH", Value: mountPath},
	}

	runAsUser := int64(0)
	containers := []corev1.Container{{
		Name:    "rclone",
		Env:     env,
		Command: []string{"/bin/bash", "-c", "./active.sh"},
		Image:   RcloneContainerImage,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: &runAsUser,
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: mountPath},
			{Name: rcloneSecret, MountPath: "/rclone-config"},
		},
	}}
	secretMode := int32(0600)
	volumes := []corev1.Volume{
		{Name: dataVolumeName, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: dataPVCName,
			}},
		},
		{Name: rcloneSecret, VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  rcloneSecretName,
				DefaultMode: &secretMode,
			}},
		},
	}

	labels := map[string]string{}
	return createOrUpdateJob(ctx, l, c, job, owner,
		scheme, labels, containers, volumes, paused, saName)
}

func envFromSecret(secretName string, field string, optional bool) corev1.EnvVar {
	return corev1.EnvVar{
		Name: field,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
				Key:      field,
				Optional: &optional,
			},
		},
	}
}

//nolint:funlen
func createOrUpdateJobRestic(ctx context.Context,
	l logr.Logger,
	c client.Client,
	job *batchv1.Job,
	owner metav1.Object,
	scheme *runtime.Scheme,
	dataPVCName string,
	cachePVCName string,
	resticSecretName string,
	forgetOptions string,
	actions []string,
	paused bool,
	saName string) (bool, error) {
	env := []corev1.EnvVar{
		{Name: "FORGET_OPTIONS", Value: forgetOptions},
		{Name: "DATA_DIR", Value: mountPath},
		{Name: "RESTIC_CACHE_DIR", Value: resticCacheMountPath},
		// We populate environment variables from the restic repo Secret. They
		// are taken 1-for-1 from the Secret into env vars. The allowed
		// variables are defined by restic.
		// https://restic.readthedocs.io/en/stable/040_backup.html#environment-variables
		// Mandatory variables are needed to define the repository location and
		// its password.
		envFromSecret(resticSecretName, "RESTIC_REPOSITORY", false),
		envFromSecret(resticSecretName, "RESTIC_PASSWORD", false),

		// Optional variables based on what backend is used for restic
		envFromSecret(resticSecretName, "AWS_ACCESS_KEY_ID", true),
		envFromSecret(resticSecretName, "AWS_SECRET_ACCESS_KEY", true),
		envFromSecret(resticSecretName, "AWS_DEFAULT_REGION", true),

		envFromSecret(resticSecretName, "ST_AUTH", true),
		envFromSecret(resticSecretName, "ST_USER", true),
		envFromSecret(resticSecretName, "ST_KEY", true),

		envFromSecret(resticSecretName, "OS_AUTH_URL", true),
		envFromSecret(resticSecretName, "OS_REGION_NAME", true),
		envFromSecret(resticSecretName, "OS_USERNAME", true),
		envFromSecret(resticSecretName, "OS_USER_ID", true),
		envFromSecret(resticSecretName, "OS_PASSWORD", true),
		envFromSecret(resticSecretName, "OS_TENANT_ID", true),
		envFromSecret(resticSecretName, "OS_TENANT_NAME", true),

		envFromSecret(resticSecretName, "OS_USER_DOMAIN_NAME", true),
		envFromSecret(resticSecretName, "OS_USER_DOMAIN_ID", true),
		envFromSecret(resticSecretName, "OS_PROJECT_NAME", true),
		envFromSecret(resticSecretName, "OS_PROJECT_DOMAIN_NAME", true),
		envFromSecret(resticSecretName, "OS_PROJECT_DOMAIN_ID", true),
		envFromSecret(resticSecretName, "OS_TRUST_ID", true),

		envFromSecret(resticSecretName, "OS_APPLICATION_CREDENTIAL_ID", true),
		envFromSecret(resticSecretName, "OS_APPLICATION_CREDENTIAL_NAME", true),
		envFromSecret(resticSecretName, "OS_APPLICATION_CREDENTIAL_SECRET", true),

		envFromSecret(resticSecretName, "OS_STORAGE_URL", true),
		envFromSecret(resticSecretName, "OS_AUTH_TOKEN", true),

		envFromSecret(resticSecretName, "B2_ACCOUNT_ID", true),
		envFromSecret(resticSecretName, "B2_ACCOUNT_KEY", true),

		envFromSecret(resticSecretName, "AZURE_ACCOUNT_NAME", true),
		envFromSecret(resticSecretName, "AZURE_ACCOUNT_KEY", true),

		envFromSecret(resticSecretName, "GOOGLE_PROJECT_ID", true),
		envFromSecret(resticSecretName, "GOOGLE_APPLICATION_CREDENTIALS", true),
	}

	runAsUser := int64(0)
	containers := []corev1.Container{{
		Name:    "restic",
		Env:     env,
		Command: []string{"/entry.sh"},
		Args:    actions,
		Image:   ResticContainerImage,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: &runAsUser,
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: mountPath},
			{Name: resticCache, MountPath: resticCacheMountPath},
		},
	}}
	volumes := []corev1.Volume{
		{Name: dataVolumeName, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: dataPVCName,
			}},
		},
		{Name: resticCache, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: cachePVCName,
			}},
		},
	}

	labels := map[string]string{}
	return createOrUpdateJob(ctx, l, c, job, owner,
		scheme, labels, containers, volumes, paused, saName)
}

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

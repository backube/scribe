/*
Copyright 2020 The Scribe authors.

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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	snapv1 "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	"github.com/operator-framework/operator-lib/status"
	"github.com/prometheus/client_golang/prometheus"
	cron "github.com/robfig/cron/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	scribev1alpha1 "github.com/backube/scribe/api/v1alpha1"
)

const (
	// mountPath is the source and destination data directory location
	mountPath            = "/data"
	resticCacheMountPath = "/cache"
)

// ReplicationSourceReconciler reconciles a ReplicationSource object
type ReplicationSourceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//nolint:lll
//nolint:funlen
//+kubebuilder:rbac:groups=scribe.backube,resources=replicationsources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=scribe.backube,resources=replicationsources/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=scribe.backube,resources=replicationsources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=scribe-mover,verbs=use
//+kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete

//nolint:funlen
func (r *ReplicationSourceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("replicationsource", req.NamespacedName)
	inst := &scribev1alpha1.ReplicationSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, inst); err != nil {
		if kerrors.IsNotFound(err) {
			logger.Error(err, "Failed to get Source")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if inst.Status == nil {
		inst.Status = &scribev1alpha1.ReplicationSourceStatus{}
	}
	if inst.Status.Conditions == nil {
		inst.Status.Conditions = status.Conditions{}
	}

	var result ctrl.Result
	var err error
	if inst.Spec.Rsync != nil && inst.Spec.Rclone != nil ||
		inst.Spec.External != nil && inst.Spec.Rclone != nil ||
		inst.Spec.Rsync != nil && inst.Spec.External != nil {
		err = fmt.Errorf("only a single replication method can be provided")
	} else if inst.Spec.Rsync != nil {
		result, err = RunRsyncSrcReconciler(ctx, inst, r, logger)
	} else if inst.Spec.Rclone != nil {
		result, err = RunRcloneSrcReconciler(ctx, inst, r, logger)
	} else if inst.Spec.Restic != nil {
		result, err = RunResticSrcReconciler(ctx, inst, r, logger)
	} else {
		return ctrl.Result{}, nil
	}

	// Set reconcile status condition
	if err == nil {
		inst.Status.Conditions.SetCondition(
			status.Condition{
				Type:    scribev1alpha1.ConditionReconciled,
				Status:  corev1.ConditionTrue,
				Reason:  scribev1alpha1.ReconciledReasonComplete,
				Message: "Reconcile complete",
			})
	} else {
		inst.Status.Conditions.SetCondition(
			status.Condition{
				Type:    scribev1alpha1.ConditionReconciled,
				Status:  corev1.ConditionFalse,
				Reason:  scribev1alpha1.ReconciledReasonError,
				Message: err.Error(),
			})
	}

	// Update instance status
	statusErr := r.Client.Status().Update(ctx, inst)
	if err == nil { // Don't mask previous error
		err = statusErr
	}
	if !inst.Status.NextSyncTime.IsZero() {
		// ensure we get re-reconciled no later than the next scheduled sync
		// time
		delta := time.Until(inst.Status.NextSyncTime.Time)
		if delta > 0 {
			result.RequeueAfter = delta
		}
	}
	return result, err
}

func (r *ReplicationSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&scribev1alpha1.ReplicationSource{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&snapv1.VolumeSnapshot{}).
		Complete(r)
}

// pastScheduleDeadline returns true if a scheduled sync hasn't been completed
// within the synchronization period.
func pastScheduleDeadline(schedule cron.Schedule, lastCompleted time.Time, now time.Time) bool {
	// Each synchronization should complete before the next scheduled start
	// time. This means that, starting from the last completed, the next sync
	// would start at last->next, and must finish before last->next->next.
	return schedule.Next(schedule.Next(lastCompleted)).Before(now)
}

//nolint:dupl
func updateNextSyncSource(rs *scribev1alpha1.ReplicationSource, metrics scribeMetrics,
	logger logr.Logger) (bool, error) {
	// if there's a schedule
	if rs.Spec.Trigger != nil && rs.Spec.Trigger.Schedule != nil {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		schedule, err := parser.Parse(*rs.Spec.Trigger.Schedule)
		if err != nil {
			logger.Error(err, "error parsing schedule", "cronspec", rs.Spec.Trigger.Schedule)
			return false, err
		}

		// If we've previously completed a sync
		if !rs.Status.LastSyncTime.IsZero() {
			if pastScheduleDeadline(schedule, rs.Status.LastSyncTime.Time, time.Now()) {
				metrics.OutOfSync.Set(1)
			}
			next := schedule.Next(rs.Status.LastSyncTime.Time)
			rs.Status.NextSyncTime = &metav1.Time{Time: next}
		} else { // Never synced before, so we should ASAP
			rs.Status.NextSyncTime = &metav1.Time{Time: time.Now()}
		}
	} else { // No schedule, so there's no "next"
		rs.Status.NextSyncTime = nil
	}

	if rs.Status.LastSyncTime.IsZero() {
		// Never synced before, so we're out of sync
		metrics.OutOfSync.Set(1)
	}

	return true, nil
}

func awaitNextSyncSource(rs *scribev1alpha1.ReplicationSource, metrics scribeMetrics,
	logger logr.Logger) (bool, error) {
	// Ensure nextSyncTime is correct
	if cont, err := updateNextSyncSource(rs, metrics, logger); !cont || err != nil {
		return cont, err
	}

	// If there's no next (no schedule) or we're past the nextSyncTime, we should sync
	if rs.Status.NextSyncTime.IsZero() || rs.Status.NextSyncTime.Time.Before(time.Now()) {
		return true, nil
	}
	return false, nil
}

//nolint:dupl
func updateLastSyncSource(rs *scribev1alpha1.ReplicationSource, metrics scribeMetrics,
	logger logr.Logger) (bool, error) {
	// if there's a schedule see if we've made the deadline
	if rs.Spec.Trigger != nil && rs.Spec.Trigger.Schedule != nil && rs.Status.LastSyncTime != nil {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		schedule, err := parser.Parse(*rs.Spec.Trigger.Schedule)
		if err != nil {
			logger.Error(err, "error parsing schedule", "cronspec", rs.Spec.Trigger.Schedule)
			return false, err
		}
		if pastScheduleDeadline(schedule, rs.Status.LastSyncTime.Time, time.Now()) {
			metrics.MissedIntervals.Inc()
		} else {
			metrics.OutOfSync.Set(0)
		}
	} else {
		// There's no schedule or we just completed our first sync, so mark as
		// in-sync
		metrics.OutOfSync.Set(0)
	}

	rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
	return updateNextSyncSource(rs, metrics, logger)
}

type rsyncSrcReconciler struct {
	sourceVolumeHandler
	scribeMetrics
	service        *corev1.Service
	destSecret     *corev1.Secret
	srcSecret      *corev1.Secret
	serviceAccount *corev1.ServiceAccount
	job            *batchv1.Job
}

type rcloneSrcReconciler struct {
	sourceVolumeHandler
	scribeMetrics
	rcloneConfigSecret *corev1.Secret
	serviceAccount     *corev1.ServiceAccount
	job                *batchv1.Job
}

type resticSrcReconciler struct {
	sourceVolumeHandler
	resticRepositorySecret *corev1.Secret
	serviceAccount         *corev1.ServiceAccount
	job                    *batchv1.Job
}

//nolint:dupl
func RunRsyncSrcReconciler(ctx context.Context, instance *scribev1alpha1.ReplicationSource,
	sr *ReplicationSourceReconciler, logger logr.Logger) (ctrl.Result, error) {
	r := rsyncSrcReconciler{
		sourceVolumeHandler: sourceVolumeHandler{
			Ctx:                         ctx,
			Instance:                    instance,
			ReplicationSourceReconciler: *sr,
			Options:                     &instance.Spec.Rsync.ReplicationSourceVolumeOptions,
		},
		scribeMetrics: newScribeMetrics(prometheus.Labels{
			"obj_name":      instance.Name,
			"obj_namespace": instance.Namespace,
			"role":          "source",
			"method":        "rsync",
		}),
	}

	l := logger.WithValues("method", "Rsync")

	// Make sure there's a place to write status info
	if r.Instance.Status.Rsync == nil {
		r.Instance.Status.Rsync = &scribev1alpha1.ReplicationSourceRsyncStatus{}
	}

	// wrap the scheduling functions as reconcileFuncs
	awaitNextSync := func(l logr.Logger) (bool, error) {
		return awaitNextSyncSource(r.Instance, r.scribeMetrics, l)
	}

	_, err := reconcileBatch(l,
		awaitNextSync,
		r.EnsurePVC,
		r.ensureService,
		r.publishSvcAddress,
		r.ensureKeys,
		r.ensureServiceAccount,
		r.ensureJob,
		r.cleanupJob,
		r.CleanupPVC,
	)
	return ctrl.Result{}, err
}

// RunRcloneSrcReconciler is invoked when ReplicationSource.Spec.Rclone != nil
func RunRcloneSrcReconciler(ctx context.Context, instance *scribev1alpha1.ReplicationSource,
	sr *ReplicationSourceReconciler, logger logr.Logger) (ctrl.Result, error) {
	r := rcloneSrcReconciler{
		sourceVolumeHandler: sourceVolumeHandler{
			Ctx:                         ctx,
			Instance:                    instance,
			ReplicationSourceReconciler: *sr,
			Options:                     &instance.Spec.Rclone.ReplicationSourceVolumeOptions,
		},
		scribeMetrics: newScribeMetrics(prometheus.Labels{
			"obj_name":      instance.Name,
			"obj_namespace": instance.Namespace,
			"role":          "source",
			"method":        "rclone",
		}),
	}

	l := logger.WithValues("method", "Rclone")

	// wrap the scheduling functions as reconcileFuncs
	awaitNextSync := func(l logr.Logger) (bool, error) {
		return awaitNextSyncSource(r.Instance, r.scribeMetrics, l)
	}

	_, err := reconcileBatch(l,
		awaitNextSync,
		r.validateRcloneSpec,
		r.EnsurePVC,
		r.ensureServiceAccount,
		r.ensureRcloneConfig,
		r.ensureJob,
		r.cleanupJob,
		r.CleanupPVC,
	)
	return ctrl.Result{}, err
}

// RunResticSrcReconciler is invokded when ReplicationSource.Spec>Restic !=  nil
func RunResticSrcReconciler(ctx context.Context, instance *scribev1alpha1.ReplicationSource,
	sr *ReplicationSourceReconciler, logger logr.Logger) (ctrl.Result, error) {
	r := resticSrcReconciler{
		sourceVolumeHandler: sourceVolumeHandler{
			Ctx:                         ctx,
			Instance:                    instance,
			ReplicationSourceReconciler: *sr,
			Options:                     &instance.Spec.Restic.ReplicationSourceVolumeOptions,
		},
	}
	l := logger.WithValues("method", "Restic")
	//Wrap the scheduling functions as reconcileFuncs
	awaitNextSync := func(l logr.Logger) (bool, error) {
		return awaitNextSyncSource(r.Instance, l)
	}

	_, err := reconcileBatch(l,
		awaitNextSync,
		r.validateResticSpec,
		r.EnsurePVC,
		r.pvcForCache,
		r.ensureServiceAccount,
		r.ensureRepository,
		r.ensureJob,
		// r.cleanupJob,
		// r.CleanupPVC,
	)
	return ctrl.Result{}, err

}

//nolint:funlen
func (r *rcloneSrcReconciler) ensureJob(l logr.Logger) (bool, error) {
	r.job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-rclone-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	logger := l.WithValues("job", nameFor(r.job))
	op, err := ctrlutil.CreateOrUpdate(r.Ctx, r.Client, r.job, func() error {
		if err := ctrl.SetControllerReference(r.Instance, r.job, r.Scheme); err != nil {
			logger.Error(err, "unable to set controller reference")
			return err
		}
		r.job.Spec.Template.ObjectMeta.Name = r.job.Name
		if r.job.Spec.Template.ObjectMeta.Labels == nil {
			r.job.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		backoffLimit := int32(2)
		r.job.Spec.BackoffLimit = &backoffLimit
		if r.Instance.Spec.Paused {
			parallelism := int32(0)
			r.job.Spec.Parallelism = &parallelism
		} else {
			parallelism := int32(1)
			r.job.Spec.Parallelism = &parallelism
		}
		if len(r.job.Spec.Template.Spec.Containers) != 1 {
			r.job.Spec.Template.Spec.Containers = []corev1.Container{{}}
		}

		r.job.Spec.Template.Spec.Containers[0].Name = "rclone"
		r.job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
			{Name: "RCLONE_CONFIG", Value: "/rclone-config/rclone.conf"},
			{Name: "RCLONE_DEST_PATH", Value: *r.Instance.Spec.Rclone.RcloneDestPath},
			{Name: "DIRECTION", Value: "source"},
			{Name: "MOUNT_PATH", Value: mountPath},
			{Name: "RCLONE_CONFIG_SECTION", Value: *r.Instance.Spec.Rclone.RcloneConfigSection},
		}
		r.job.Spec.Template.Spec.Containers[0].Command = []string{"/bin/bash", "-c", "./active.sh"}
		r.job.Spec.Template.Spec.Containers[0].Image = RcloneContainerImage
		runAsUser := int64(0)
		r.job.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			RunAsUser: &runAsUser,
		}
		r.job.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: mountPath},
			{Name: rcloneSecret, MountPath: "/rclone-config/"},
		}
		r.job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		r.job.Spec.Template.Spec.ServiceAccountName = r.serviceAccount.Name
		secretMode := int32(0600)
		r.job.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: dataVolumeName, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.PVC.Name,
				}},
			},
			{Name: rcloneSecret, VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  r.rcloneConfigSecret.Name,
					DefaultMode: &secretMode,
				}},
			},
		}
		logger.V(1).Info("Job has PVC", "PVC", r.PVC, "DS", r.PVC.Spec.DataSource)
		return nil
	})
	// If Job had failed, delete it so it can be recreated
	if r.job.Status.Failed == *r.job.Spec.BackoffLimit {
		logger.Info("deleting job -- backoff limit reached")
		err = r.Client.Delete(r.Ctx, r.job, client.PropagationPolicy(metav1.DeletePropagationBackground))
		return false, err
	}
	if err != nil {
		logger.Error(err, "reconcile failed")
	} else {
		logger.V(1).Info("Job reconciled", "operation", op)
	}
	// We only continue reconciling if the rclone job has completed
	return r.job.Status.Succeeded == 1, nil
}

//nolint:funlen
func (r *resticSrcReconciler) ensureJob(l logr.Logger) (bool, error) {
	r.job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-restic-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	logger := l.WithValues("job", nameFor(r.job))
	op, err := ctrlutil.CreateOrUpdate(r.Ctx, r.Client, r.job, func() error {
		if err := ctrl.SetControllerReference(r.Instance, r.job, r.Scheme); err != nil {
			logger.Error(err, "unable to set controller reference")
			return err
		}
		r.job.Spec.Template.ObjectMeta.Name = r.job.Name
		if r.job.Spec.Template.ObjectMeta.Labels == nil {
			r.job.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		backoffLimit := int32(2)
		r.job.Spec.BackoffLimit = &backoffLimit
		if r.Instance.Spec.Paused {
			parallelism := int32(0)
			r.job.Spec.Parallelism = &parallelism
		} else {
			parallelism := int32(1)
			r.job.Spec.Parallelism = &parallelism
		}
		if len(r.job.Spec.Template.Spec.Containers) != 1 {
			r.job.Spec.Template.Spec.Containers = []corev1.Container{{}}
		}

		r.job.Spec.Template.Spec.Containers[0].Name = "restic"
		// calculate retention policy. for now setting FORGET_OPTIONS in
		// env variables directly. It has to be calculated from retention
		// policy

		// get secret from cluster
		foundSecret := &corev1.Secret{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: resticSecret, Namespace: r.Instance.Namespace}, foundSecret)
		if err == nil {
			dataMap := *&foundSecret.Data
			logger.Info("-------------- restic sescert-------", "dataMap", string(dataMap["RESTIC_REPOSITORY"]))
			r.job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
				{Name: "FORGET_OPTIONS", Value: "--keep-hourly 2 --keep-daily 1"},
				{Name: "DATA_DIR", Value: mountPath},
				{Name: "RESTIC_CACHE_DIR", Value: resticCacheMountPath},
				{Name: "RESTIC_REPOSITORY", Value: string(dataMap["RESTIC_REPOSITORY"])},
				{Name: "RESTIC_PASSWORD", Value: string(dataMap["RESTIC_PASSWORD"])},
				{Name: "AWS_ACCESS_KEY_ID", Value: string(dataMap["AWS_ACCESS_KEY_ID"])},
				{Name: "AWS_SECRET_ACCESS_KEY", Value: string(dataMap["AWS_SECRET_ACCESS_KEY"])},
			}

		} else {
			logger.Error(err, "Loading env variables to Job failed")
			return err

		}

		r.job.Spec.Template.Spec.Containers[0].Command = []string{"/bin/bash", "-c", "./entry.sh"}
		r.job.Spec.Template.Spec.Containers[0].Args = []string{"backup"}
		r.job.Spec.Template.Spec.Containers[0].Image = "quay.io/husky_parul/scribe-mover-restic:latest"
		runAsUser := int64(0)
		r.job.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			RunAsUser: &runAsUser,
		}
		r.job.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: mountPath},
			{Name: resticCache, MountPath: resticCacheMountPath},
		}
		r.job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		r.job.Spec.Template.Spec.ServiceAccountName = r.serviceAccount.Name
		// secretMode := int32(0600)
		r.job.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: dataVolumeName, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.PVC.Name,
				}},
			},
			{Name: resticCache, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.resticCache.Name,
				}},
			},
		}
		logger.V(1).Info("Job has PVC", "PVC", r.PVC, "DS", r.PVC.Spec.DataSource)
		return nil
	})
	// If Job had failed, delete it so it can be recreated
	if r.job.Status.Failed == *r.job.Spec.BackoffLimit {
		logger.Info("deleting job -- backoff limit reached")
		err = r.Client.Delete(r.Ctx, r.job, client.PropagationPolicy(metav1.DeletePropagationBackground))
		return false, err
	}
	if err != nil {
		logger.Error(err, "reconcile failed")
	} else {
		logger.V(1).Info("Job reconciled", "operation", op)
	}
	// We only continue reconciling if the restic job has completed
	return r.job.Status.Succeeded == 1, nil
}

//nolint:funlen
// func (r *resticSrcReconciler) ensureRetain(l logr.Logger) (bool, error) {
// 	startTime := *&r.Instance.Status.
// 	h := time.Now().Hour()
// 	interval := int64(*r.Instance.Spec.Restic.PrueIntervalDays * 24)

// 	if r.Instance.Status.LastPruned == nil {
// 		// this is the first backup and never has been pruned.
// 		hours := time.Duration(interval) * time.Hour

// 	}
// 	if r.Instance.Status.LastPruned != nil {
// 		//calculate next prune time as now - lastPruned > pruneInterval
// 		// if true: call prune
// 		// set last prune time
// 	}

// 	return true, nil

// }

func (r *rsyncSrcReconciler) serviceSelector() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "src-" + r.Instance.Name,
		"app.kubernetes.io/component": "rsync-mover",
		"app.kubernetes.io/part-of":   "scribe",
	}
}

// ensureService maintains the Service that is used to connect to the
// source rsync mover.
func (r *rsyncSrcReconciler) ensureService(l logr.Logger) (bool, error) {
	if r.Instance.Spec.Rsync.Address != nil {
		// Connection will be outbound. Don't need a Service
		return true, nil
	}

	r.service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-rsync-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	svcDesc := rsyncSvcDescription{
		Context:  r.Ctx,
		Client:   r.Client,
		Scheme:   r.Scheme,
		Service:  r.service,
		Owner:    r.Instance,
		Type:     r.Instance.Spec.Rsync.ServiceType,
		Selector: r.serviceSelector(),
		Port:     r.Instance.Spec.Rsync.Port,
	}
	return svcDesc.Reconcile(l)
}

func (r *rsyncSrcReconciler) publishSvcAddress(l logr.Logger) (bool, error) {
	if r.service == nil { // no service, nothing to do
		r.Instance.Status.Rsync.Address = nil
		return true, nil
	}

	address := getServiceAddress(r.service)
	if address == "" {
		// We don't have an address yet, try again later
		r.Instance.Status.Rsync.Address = nil
		return false, nil
	}
	r.Instance.Status.Rsync.Address = &address

	l.V(1).Info("Service addr published", "address", address)
	return true, nil
}

//nolint:dupl
func (r *rsyncSrcReconciler) ensureKeys(l logr.Logger) (bool, error) {
	// If user provided keys, use those
	if r.Instance.Spec.Rsync.SSHKeys != nil {
		r.srcSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      *r.Instance.Spec.Rsync.SSHKeys,
				Namespace: r.Instance.Namespace,
			},
		}
		fields := []string{"source", "source.pub", "destination.pub"}
		if err := getAndValidateSecret(r.Ctx, r.Client, l, r.srcSecret, fields); err != nil {
			l.Error(err, "SSH keys secret does not contain the proper fields")
			return false, err
		}
		return true, nil
	}

	// otherwise, we need to create our own
	keyInfo := rsyncSSHKeys{
		Context:      r.Ctx,
		Client:       r.Client,
		Scheme:       r.Scheme,
		Owner:        r.Instance,
		NameTemplate: "scribe-rsync-src",
	}
	cont, err := keyInfo.Reconcile(l)
	if !cont || err != nil {
		r.Instance.Status.Rsync.SSHKeys = nil
	} else {
		r.srcSecret = keyInfo.SrcSecret
		r.destSecret = keyInfo.DestSecret
		r.Instance.Status.Rsync.SSHKeys = &r.destSecret.Name
	}
	return cont, err
}

func (r *rcloneSrcReconciler) ensureRcloneConfig(l logr.Logger) (bool, error) {
	// If user provided "rclone-secret", use those

	r.rcloneConfigSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      *r.Instance.Spec.Rclone.RcloneConfig,
			Namespace: r.Instance.Namespace,
		},
	}
	fields := []string{"rclone.conf"}
	if err := getAndValidateSecret(r.Ctx, r.Client, l, r.rcloneConfigSecret, fields); err != nil {
		l.Error(err, "Rclone config secret does not contain the proper fields")
		return false, err
	}
	return true, nil
}

func (r *resticSrcReconciler) ensureRepository(l logr.Logger) (bool, error) {
	// If user provided "repository-secret", use those

	r.resticRepositorySecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      *r.Instance.Spec.Restic.Repository,
			Namespace: r.Instance.Namespace,
		},
	}
	fields := []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "RESTIC_PASSWORD", "RESTIC_REPOSITORY"}
	if err := getAndValidateSecret(r.Ctx, r.Client, l, r.resticRepositorySecret, fields); err != nil {
		l.Error(err, "Restic config secret does not contain the proper fields")
		return false, err
	}
	return true, nil
}

func (r *rsyncSrcReconciler) ensureServiceAccount(l logr.Logger) (bool, error) {
	r.serviceAccount = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-rsync-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	saDesc := rsyncSADescription{
		Context: r.Ctx,
		Client:  r.Client,
		Scheme:  r.Scheme,
		SA:      r.serviceAccount,
		Owner:   r.Instance,
	}
	return saDesc.Reconcile(l)
}

func (r *rcloneSrcReconciler) ensureServiceAccount(l logr.Logger) (bool, error) {
	r.serviceAccount = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	saDesc := rsyncSADescription{
		Context: r.Ctx,
		Client:  r.Client,
		Scheme:  r.Scheme,
		SA:      r.serviceAccount,
		Owner:   r.Instance,
	}
	return saDesc.Reconcile(l)
}

func (r *resticSrcReconciler) ensureServiceAccount(l logr.Logger) (bool, error) {
	r.serviceAccount = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	saDesc := rsyncSADescription{
		Context: r.Ctx,
		Client:  r.Client,
		Scheme:  r.Scheme,
		SA:      r.serviceAccount,
		Owner:   r.Instance,
	}
	return saDesc.Reconcile(l)
}

//nolint:funlen
func (r *rsyncSrcReconciler) ensureJob(l logr.Logger) (bool, error) {
	r.job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scribe-rsync-src-" + r.Instance.Name,
			Namespace: r.Instance.Namespace,
		},
	}
	logger := l.WithValues("job", nameFor(r.job))

	op, err := ctrlutil.CreateOrUpdate(r.Ctx, r.Client, r.job, func() error {
		if err := ctrl.SetControllerReference(r.Instance, r.job, r.Scheme); err != nil {
			logger.Error(err, "unable to set controller reference")
			return err
		}
		r.job.Spec.Template.ObjectMeta.Name = r.job.Name
		if r.job.Spec.Template.ObjectMeta.Labels == nil {
			r.job.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		for k, v := range r.serviceSelector() {
			r.job.Spec.Template.ObjectMeta.Labels[k] = v
		}
		backoffLimit := int32(2)
		r.job.Spec.BackoffLimit = &backoffLimit
		if r.Instance.Spec.Paused {
			parallelism := int32(0)
			r.job.Spec.Parallelism = &parallelism
		} else {
			parallelism := int32(1)
			r.job.Spec.Parallelism = &parallelism
		}
		if len(r.job.Spec.Template.Spec.Containers) != 1 {
			r.job.Spec.Template.Spec.Containers = []corev1.Container{{}}
		}
		r.job.Spec.Template.Spec.Containers[0].Name = "rsync"
		if r.Instance.Spec.Rsync.Port != nil && r.Instance.Spec.Rsync.Address != nil {
			connectPort := strconv.Itoa(int(*r.Instance.Spec.Rsync.Port))
			r.job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
				{Name: "DESTINATION_ADDRESS", Value: *r.Instance.Spec.Rsync.Address},
				{Name: "DESTINATION_PORT", Value: connectPort},
			}
		} else if r.Instance.Spec.Rsync.Port == nil && r.Instance.Spec.Rsync.Address != nil {
			r.job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
				{Name: "DESTINATION_ADDRESS", Value: *r.Instance.Spec.Rsync.Address},
			}
		} else if r.Instance.Spec.Rsync.Address == nil {
			r.job.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{}
		}
		r.job.Spec.Template.Spec.Containers[0].Command = []string{"/bin/bash", "-c", "/source.sh"}
		r.job.Spec.Template.Spec.Containers[0].Image = RsyncContainerImage
		runAsUser := int64(0)
		r.job.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"AUDIT_WRITE",
					"SYS_CHROOT",
				},
			},
			RunAsUser: &runAsUser,
		}
		r.job.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: mountPath},
			{Name: "keys", MountPath: "/keys"},
		}
		r.job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		r.job.Spec.Template.Spec.ServiceAccountName = r.serviceAccount.Name
		secretMode := int32(0600)
		r.job.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: dataVolumeName, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.PVC.Name,
				}},
			},
			{Name: "keys", VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  r.srcSecret.Name,
					DefaultMode: &secretMode,
				}},
			},
		}
		logger.V(1).Info("Job has PVC", "PVC", r.PVC, "DS", r.PVC.Spec.DataSource)
		return nil
	})

	// If Job had failed, delete it so it can be recreated
	if r.job.Status.Failed == *r.job.Spec.BackoffLimit {
		logger.Info("deleting job -- backoff limit reached")
		err = r.Client.Delete(r.Ctx, r.job, client.PropagationPolicy(metav1.DeletePropagationBackground))
		return false, err
	}

	if err != nil {
		logger.Error(err, "reconcile failed")
	} else {
		logger.V(1).Info("Job reconciled", "operation", op)
	}

	// We only continue reconciling if the rsync job has completed
	return r.job.Status.Succeeded == 1, nil
}

//nolint:dupl
func (r *rsyncSrcReconciler) cleanupJob(l logr.Logger) (bool, error) {
	logger := l.WithValues("job", r.job)
	// update time/duration
	if cont, err := updateLastSyncSource(r.Instance, r.scribeMetrics, logger); !cont || err != nil {
		return cont, err
	}
	if r.job.Status.StartTime != nil {
		d := r.Instance.Status.LastSyncTime.Sub(r.job.Status.StartTime.Time)
		r.Instance.Status.LastSyncDuration = &metav1.Duration{Duration: d}
		r.SyncDurations.Observe(d.Seconds())
	}
	// remove job
	if err := r.Client.Delete(r.Ctx, r.job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
		logger.Error(err, "unable to delete job")
		return false, err
	}
	return true, nil
}

func (r *rcloneSrcReconciler) cleanupJob(l logr.Logger) (bool, error) {
	logger := l.WithValues("job", r.job)
	// update time/duration
	if cont, err := updateLastSyncSource(r.Instance, r.scribeMetrics, logger); !cont || err != nil {
		return cont, err
	}
	if r.job.Status.StartTime != nil {
		d := r.Instance.Status.LastSyncTime.Sub(r.job.Status.StartTime.Time)
		r.Instance.Status.LastSyncDuration = &metav1.Duration{Duration: d}
		r.SyncDurations.Observe(d.Seconds())
	}
	// remove job
	if r.job.Status.Succeeded >= 1 {
		if err := r.Client.Delete(r.Ctx, r.job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			logger.Error(err, "unable to delete job")
			return false, err
		}
		logger.Info("Job deleted", "Job name: ", r.job.Spec.Template.ObjectMeta.Name)
	}
	return true, nil
}

func (r *resticSrcReconciler) cleanupJob(l logr.Logger) (bool, error) {
	logger := l.WithValues("job", r.job)
	// update time/duration
	if _, err := updateLastSyncSource(r.Instance, logger); err != nil {
		return false, err
	}
	if r.job.Status.StartTime != nil {
		d := r.Instance.Status.LastSyncTime.Sub(r.job.Status.StartTime.Time)
		r.Instance.Status.LastSyncDuration = &metav1.Duration{Duration: d}
	}
	// remove job
	if r.job.Status.Succeeded >= 1 {
		if err := r.Client.Delete(r.Ctx, r.job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			logger.Error(err, "unable to delete job")
			return false, err
		}
		logger.Info("Job deleted", "Job name: ", r.job.Spec.Template.ObjectMeta.Name)
	}
	return true, nil
}

func (r *rcloneSrcReconciler) validateRcloneSpec(l logr.Logger) (bool, error) {
	if len(*r.Instance.Spec.Rclone.RcloneConfig) == 0 {
		err := errors.New("Unable to get Rclone config secret name")
		l.V(1).Info("Unable to get Rclone config secret name")
		return false, err
	}
	if len(*r.Instance.Spec.Rclone.RcloneConfigSection) == 0 {
		err := errors.New("Unable to get Rclone config section name")
		l.V(1).Info("Unable to get Rclone config section name")

		return false, err
	}
	if len(*r.Instance.Spec.Rclone.RcloneDestPath) == 0 {
		err := errors.New("Unable to get Rclone destination name")
		l.V(1).Info("Unable to get Rclone destination name")

		return false, err
	}
	return true, nil
}

func (r *resticSrcReconciler) validateResticSpec(l logr.Logger) (bool, error) {
	if len(*r.Instance.Spec.Restic.Repository) == 0 {
		err := errors.New("Unable to get restic repository configurations")
		l.V(1).Info("Unable to get restic repository configurations")
		return false, err
	}
	if r.Instance.Spec.Restic.PruneIntervalDays == nil {
		err := errors.New("Unable to get restic pruneIntervalDays")
		l.V(1).Info("Unable to get restic pruneIntervalDays")

		return false, err
	}
	if r.Instance.Spec.Restic.Retain == nil {
		err := errors.New("Unable to get restic retain policy")
		l.V(1).Info("Unable to get restic retain policy")

		return false, err
	}

	return true, nil
}

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	projectmassopencloudv1alpha1 "cci-moc.github.com/moc-project-operator/api/v1alpha1"
)

const (
	finalizerName       = "project.massopen.cloud/finalizer"
	labelProjectName    = "project.massopen.cloud/name"
	labelProjectDeleted = "project.massopen.cloud/deleted"
)

var groupGVK = schema.GroupVersionKind{
	Group:   "user.openshift.io",
	Version: "v1",
	Kind:    "Group",
}

type ProjectReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	ControllerNamespace string
}

// +kubebuilder:rbac:groups=project.massopen.cloud,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=project.massopen.cloud,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=project.massopen.cloud,resources=projects/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=limitranges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=user.openshift.io,resources=groups,verbs=get;list;watch;create;update;patch;delete

func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	project := &projectmassopencloudv1alpha1.Project{}
	if err := r.Get(ctx, req.NamespacedName, project); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("reconciling project", "name", project.Name)

	if project.Spec.DeleteAction == "" {
		project.Spec.DeleteAction = projectmassopencloudv1alpha1.DeleteActionPreserve
		if err := r.Update(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
	}

	if project.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, project)
	}

	if !controllerutil.ContainsFinalizer(project, finalizerName) {
		controllerutil.AddFinalizer(project, finalizerName)
		if err := r.Update(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseProgressing); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNamespace(ctx, project); err != nil {
		_ = r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseFailed)
		return ctrl.Result{}, err
	}
	if err := r.reconcileGroup(ctx, project); err != nil {
		_ = r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseFailed)
		return ctrl.Result{}, err
	}
	if err := r.reconcileRoleBinding(ctx, project); err != nil {
		_ = r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseFailed)
		return ctrl.Result{}, err
	}
	if err := r.reconcileLimitRange(ctx, project); err != nil {
		_ = r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseFailed)
		return ctrl.Result{}, err
	}
	if err := r.reconcileResourceQuota(ctx, project); err != nil {
		_ = r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseFailed)
		return ctrl.Result{}, err
	}

	if err := r.setPhase(ctx, project, projectmassopencloudv1alpha1.PhaseReady); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) setPhase(ctx context.Context, project *projectmassopencloudv1alpha1.Project, phase projectmassopencloudv1alpha1.Phase) error {
	if project.Status.Phase == phase {
		return nil
	}
	project.Status.Phase = phase
	return r.Status().Update(ctx, project)
}

func (r *ProjectReconciler) handleDeletion(ctx context.Context, project *projectmassopencloudv1alpha1.Project) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(project, finalizerName) {
		return ctrl.Result{}, nil
	}

	ns := &corev1.Namespace{}
	if err := r.Get(ctx, client.ObjectKey{Name: project.Name}, ns); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	} else {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[labelProjectDeleted] = "true"
		if err := r.Update(ctx, ns); err != nil {
			return ctrl.Result{}, err
		}
	}

	switch project.Spec.DeleteAction {
	case projectmassopencloudv1alpha1.DeleteActionIgnore:
		// no additional cleanup
	case projectmassopencloudv1alpha1.DeleteActionDelete:
		if err := r.deleteGroup(ctx, project.Name); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.deleteNamespace(ctx, project.Name); err != nil {
			return ctrl.Result{}, err
		}
	case projectmassopencloudv1alpha1.DeleteActionPreserve:
		if err := r.deleteGroup(ctx, project.Name); err != nil {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(project, finalizerName)
	if err := r.Update(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) deleteGroup(ctx context.Context, name string) error {
	group := &unstructured.Unstructured{}
	group.SetGroupVersionKind(groupGVK)
	group.SetName(name)
	if err := r.Delete(ctx, group); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ProjectReconciler) deleteNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if err := r.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ProjectReconciler) reconcileNamespace(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels[labelProjectName] = project.Name
		delete(ns.Labels, labelProjectDeleted)
		return nil
	})
	return err
}

func (r *ProjectReconciler) reconcileGroup(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	group := &unstructured.Unstructured{}
	group.SetGroupVersionKind(groupGVK)
	group.SetName(project.Name)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, group, func() error {
		labels := group.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[labelProjectName] = project.Name
		group.SetLabels(labels)
		users := make([]any, len(project.Spec.Members))
		for i, m := range project.Spec.Members {
			users[i] = m
		}
		group.Object["users"] = users
		return nil
	})
	return err
}

func (r *ProjectReconciler) reconcileRoleBinding(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name,
			Namespace: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		if rb.Labels == nil {
			rb.Labels = make(map[string]string)
		}
		rb.Labels[labelProjectName] = project.Name
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "edit",
		}
		rb.Subjects = []rbacv1.Subject{
			{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Group",
				Name:     project.Name,
			},
		}
		return nil
	})
	return err
}

func (r *ProjectReconciler) reconcileResourceQuota(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name,
			Namespace: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rq, func() error {
		if rq.Labels == nil {
			rq.Labels = make(map[string]string)
		}
		rq.Labels[labelProjectName] = project.Name
		rq.Spec.Hard = make(corev1.ResourceList, len(project.Spec.Quotas))
		for k, v := range project.Spec.Quotas {
			rq.Spec.Hard[corev1.ResourceName(k)] = v
		}
		return nil
	})
	return err
}

func (r *ProjectReconciler) reconcileLimitRange(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name,
			Namespace: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, lr, func() error {
		if lr.Labels == nil {
			lr.Labels = make(map[string]string)
		}
		lr.Labels[labelProjectName] = project.Name
		lr.Spec.Limits = project.Spec.Limits
		return nil
	})
	return err
}

func (r *ProjectReconciler) mapChildToProject(ctx context.Context, obj client.Object) []reconcile.Request {
	name, ok := obj.GetLabels()[labelProjectName]
	if !ok {
		return nil
	}
	log := logf.FromContext(ctx)
	gvks, _, _ := r.Scheme.ObjectKinds(obj)
	kind := "unknown"
	if len(gvks) > 0 {
		kind = gvks[0].Kind
	}
	log.Info("triggered by child resource", "project", name, "kind", kind, "name", obj.GetName())
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: r.ControllerNamespace,
		}},
	}
}

func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if _, err := mgr.GetRESTMapper().RESTMapping(groupGVK.GroupKind(), groupGVK.Version); err != nil {
		return fmt.Errorf("required API %s not available: %w", groupGVK, err)
	}

	group := &unstructured.Unstructured{}
	group.SetGroupVersionKind(groupGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(&projectmassopencloudv1alpha1.Project{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.mapChildToProject)).
		Watches(group, handler.EnqueueRequestsFromMapFunc(r.mapChildToProject)).
		Watches(&rbacv1.RoleBinding{}, handler.EnqueueRequestsFromMapFunc(r.mapChildToProject)).
		Watches(&corev1.LimitRange{}, handler.EnqueueRequestsFromMapFunc(r.mapChildToProject)).
		Watches(&corev1.ResourceQuota{}, handler.EnqueueRequestsFromMapFunc(r.mapChildToProject)).
		Named("project").
		Complete(r)
}

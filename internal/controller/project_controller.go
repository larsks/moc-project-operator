package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

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
	Scheme *runtime.Scheme
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

	if err := r.reconcileNamespace(ctx, project); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileGroup(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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
		return nil
	})
	return err
}

func (r *ProjectReconciler) reconcileGroup(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	group := &unstructured.Unstructured{}
	group.SetGroupVersionKind(groupGVK)
	group.SetName(project.Name)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, group, func() error {
		users := make([]any, len(project.Spec.Members))
		for i, m := range project.Spec.Members {
			users[i] = m
		}
		group.Object["users"] = users
		return nil
	})
	return err
}

func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&projectmassopencloudv1alpha1.Project{}).
		Named("project").
		Complete(r)
}

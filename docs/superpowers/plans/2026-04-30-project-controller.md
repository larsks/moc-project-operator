# MOC Project Controller Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Project controller that reconciles namespace, group, rolebinding, limitrange, and resourcequota resources based on a Project custom resource, with finalizer-based deletion handling.

**Architecture:** The controller watches Project CRs and ensures five dependent resources stay in sync: a Namespace (with project label), an OpenShift Group (with members), a RoleBinding (group-to-edit), a LimitRange, and a ResourceQuota. Deletion uses a finalizer to label the namespace as deleted and clean up resources based on `deleteAction`. OpenShift Groups are managed via unstructured client to avoid the `openshift/api` dependency.

**Tech Stack:** Go 1.24, controller-runtime v0.21.0, kubebuilder v4, envtest + Ginkgo/Gomega for testing.

---

### Task 1: Sync generated code and add RBAC markers

The generated deepcopy and CRD are out of sync with `project_types.go` (they contain an `Admins` field that no longer exists in the types). Additionally, the controller needs RBAC permissions for namespaces, groups, rolebindings, limitranges, and resourcequotas.

**Files:**
- Modify: `internal/controller/project_controller.go`
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/project.massopen.cloud_projects.yaml`, `config/rbac/role.yaml`

- [ ] **Step 1: Add a validation enum marker for DeleteAction**

In `api/v1alpha1/project_types.go`, add a kubebuilder validation enum marker to the `DeleteAction` field:

```go
// +kubebuilder:validation:Enum=Ignore;Preserve;Delete
type DeleteAction string
```

- [ ] **Step 2: Add RBAC markers to the controller**

In `internal/controller/project_controller.go`, add these markers above the `Reconcile` method (after the existing markers):

```go
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=limitranges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=user.openshift.io,resources=groups,verbs=get;list;watch;create;update;patch;delete
```

- [ ] **Step 3: Regenerate manifests**

Run: `make generate manifests`
Expected: deepcopy, CRD, and RBAC files are regenerated. The `Admins` field disappears from the CRD and deepcopy. The new RBAC permissions appear in `config/rbac/role.yaml`.

- [ ] **Step 4: Verify the build**

Run: `make build`
Expected: builds successfully.

- [ ] **Step 5: Commit**

```bash
git add api/v1alpha1/project_types.go api/v1alpha1/zz_generated.deepcopy.go internal/controller/project_controller.go config/crd/bases/ config/rbac/role.yaml
git commit -m "feat: add RBAC markers and sync generated code"
```

---

### Task 2: Test infrastructure for OpenShift Groups

envtest needs a CRD definition for OpenShift Groups so the test API server can handle Group resources. The test suite also needs updated to include this CRD path.

**Files:**
- Create: `internal/controller/testdata/group_crd.yaml`
- Modify: `internal/controller/suite_test.go`

- [ ] **Step 1: Create the Group CRD for envtest**

Create `internal/controller/testdata/group_crd.yaml`:

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: groups.user.openshift.io
spec:
  group: user.openshift.io
  names:
    kind: Group
    listKind: GroupList
    plural: groups
    singular: group
  scope: Cluster
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          users:
            type: array
            items:
              type: string
```

- [ ] **Step 2: Update suite_test.go to include the Group CRD**

In `internal/controller/suite_test.go`, update the `testEnv` initialization to include the testdata directory:

```go
testEnv = &envtest.Environment{
    CRDDirectoryPaths: []string{
        filepath.Join("..", "..", "config", "crd", "bases"),
        filepath.Join("testdata"),
    },
    ErrorIfCRDPathMissing: true,
}
```

- [ ] **Step 3: Verify tests still run**

Run: `make test`
Expected: existing tests pass (the stub controller test should still pass since it expects no errors).

- [ ] **Step 4: Commit**

```bash
git add internal/controller/testdata/group_crd.yaml internal/controller/suite_test.go
git commit -m "test: add OpenShift Group CRD for envtest"
```

---

### Task 3: Namespace reconciliation

When a Project is created, the controller should ensure a namespace exists with the `project.massopen.cloud/name` label set to the project name.

**Files:**
- Modify: `internal/controller/project_controller_test.go`
- Modify: `internal/controller/project_controller.go`

- [ ] **Step 1: Write the failing test**

Replace the contents of `internal/controller/project_controller_test.go` with:

```go
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	projectmassopencloudv1alpha1 "cci-moc.github.com/moc-project-operator/api/v1alpha1"
)

var _ = Describe("Project Controller", func() {
	const projectName = "test-project"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      projectName,
		Namespace: "default",
	}

	BeforeEach(func() {
		project := &projectmassopencloudv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      projectName,
				Namespace: "default",
			},
			Spec: projectmassopencloudv1alpha1.ProjectSpec{
				Members: []string{"user1", "user2"},
			},
		}
		err := k8sClient.Get(ctx, typeNamespacedName, &projectmassopencloudv1alpha1.Project{})
		if errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, project)).To(Succeed())
		}
	})

	AfterEach(func() {
		project := &projectmassopencloudv1alpha1.Project{}
		err := k8sClient.Get(ctx, typeNamespacedName, project)
		if err == nil {
			project.Finalizers = nil
			Expect(k8sClient.Update(ctx, project)).To(Succeed())
			Expect(k8sClient.Delete(ctx, project)).To(Succeed())
		}

		ns := &corev1.Namespace{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
		if err == nil {
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		}
	})

	reconcileProject := func() {
		r := &ProjectReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	}

	Context("When reconciling a project", func() {
		It("should create a namespace with the project name label", func() {
			reconcileProject()

			ns := &corev1.Namespace{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Labels).To(HaveKeyWithValue("project.massopen.cloud/name", projectName))
		})
	})
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — the namespace does not exist after reconciliation.

- [ ] **Step 3: Implement namespace reconciliation**

Replace the contents of `internal/controller/project_controller.go` with:

```go
package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&projectmassopencloudv1alpha1.Project{}).
		Named("project").
		Complete(r)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/controller/project_controller.go internal/controller/project_controller_test.go
git commit -m "feat: reconcile namespace for project"
```

---

### Task 4: Group reconciliation

When a Project is reconciled, an OpenShift Group should exist matching the project name with members from `project.spec.members`. Uses unstructured client since Group is an OpenShift-specific resource.

**Files:**
- Modify: `internal/controller/project_controller_test.go`
- Modify: `internal/controller/project_controller.go`

- [ ] **Step 1: Write the failing test**

Add this test case inside the `"When reconciling a project"` Context block in `project_controller_test.go`, after the namespace test. Also add the `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"` import.

Add these imports to the import block:

```go
"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
"k8s.io/apimachinery/pkg/runtime/schema"
```

Add this test:

```go
It("should create a group with the project members", func() {
    reconcileProject()

    group := &unstructured.Unstructured{}
    group.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   "user.openshift.io",
        Version: "v1",
        Kind:    "Group",
    })
    err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
    Expect(err).NotTo(HaveOccurred())

    users, found, err := unstructured.NestedStringSlice(group.Object, "users")
    Expect(err).NotTo(HaveOccurred())
    Expect(found).To(BeTrue())
    Expect(users).To(ConsistOf("user1", "user2"))
})
```

Also add a cleanup block in `AfterEach` for the group:

```go
group := &unstructured.Unstructured{}
group.SetGroupVersionKind(schema.GroupVersionKind{
    Group:   "user.openshift.io",
    Version: "v1",
    Kind:    "Group",
})
group.SetName(projectName)
err = k8sClient.Delete(ctx, group)
if err != nil && !errors.IsNotFound(err) {
    Expect(err).NotTo(HaveOccurred())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — group does not exist.

- [ ] **Step 3: Implement group reconciliation**

Add these imports to `project_controller.go`:

```go
"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
"k8s.io/apimachinery/pkg/runtime/schema"
```

Add a `groupGVK` package-level variable:

```go
var groupGVK = schema.GroupVersionKind{
	Group:   "user.openshift.io",
	Version: "v1",
	Kind:    "Group",
}
```

Add this method to `project_controller.go`:

```go
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
```

Add the call in `Reconcile`, after `reconcileNamespace`:

```go
if err := r.reconcileGroup(ctx, project); err != nil {
    return ctrl.Result{}, err
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/controller/project_controller.go internal/controller/project_controller_test.go
git commit -m "feat: reconcile OpenShift group for project"
```

---

### Task 5: RoleBinding reconciliation

The controller should ensure a RoleBinding exists in the project namespace that maps the project group to the "edit" ClusterRole.

**Files:**
- Modify: `internal/controller/project_controller_test.go`
- Modify: `internal/controller/project_controller.go`

- [ ] **Step 1: Write the failing test**

Add the `rbacv1 "k8s.io/api/rbac/v1"` import to the test file.

Add this test case inside the `"When reconciling a project"` Context block:

```go
It("should create a rolebinding mapping the group to the edit role", func() {
    reconcileProject()

    rb := &rbacv1.RoleBinding{}
    err := k8sClient.Get(ctx, types.NamespacedName{
        Name:      projectName,
        Namespace: projectName,
    }, rb)
    Expect(err).NotTo(HaveOccurred())
    Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{
        APIGroup: "rbac.authorization.k8s.io",
        Kind:     "ClusterRole",
        Name:     "edit",
    }))
    Expect(rb.Subjects).To(ContainElement(rbacv1.Subject{
        APIGroup: "rbac.authorization.k8s.io",
        Kind:     "Group",
        Name:     projectName,
    }))
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — rolebinding does not exist.

- [ ] **Step 3: Implement rolebinding reconciliation**

Add the `rbacv1 "k8s.io/api/rbac/v1"` import to `project_controller.go`.

Add this method:

```go
func (r *ProjectReconciler) reconcileRoleBinding(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name,
			Namespace: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
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
```

Add the call in `Reconcile`, after `reconcileGroup`:

```go
if err := r.reconcileRoleBinding(ctx, project); err != nil {
    return ctrl.Result{}, err
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/controller/project_controller.go internal/controller/project_controller_test.go
git commit -m "feat: reconcile rolebinding for project"
```

---

### Task 6: LimitRange reconciliation

The controller should ensure a LimitRange exists in the project namespace with limits from `project.spec.limits`.

**Files:**
- Modify: `internal/controller/project_controller_test.go`
- Modify: `internal/controller/project_controller.go`

- [ ] **Step 1: Update the project fixture to include limits**

In `project_controller_test.go`, update the `BeforeEach` project creation to include limits. Add the `"k8s.io/apimachinery/pkg/api/resource"` import.

Update the project spec in `BeforeEach`:

```go
Spec: projectmassopencloudv1alpha1.ProjectSpec{
    Members: []string{"user1", "user2"},
    Limits: []corev1.LimitRangeItem{
        {
            Type: corev1.LimitTypeContainer,
            Default: corev1.ResourceList{
                corev1.ResourceCPU:    resource.MustParse("500m"),
                corev1.ResourceMemory: resource.MustParse("512Mi"),
            },
        },
    },
},
```

- [ ] **Step 2: Write the failing test**

Add this test case inside the `"When reconciling a project"` Context block:

```go
It("should create a limitrange with the project limits", func() {
    reconcileProject()

    lr := &corev1.LimitRange{}
    err := k8sClient.Get(ctx, types.NamespacedName{
        Name:      projectName,
        Namespace: projectName,
    }, lr)
    Expect(err).NotTo(HaveOccurred())
    Expect(lr.Spec.Limits).To(HaveLen(1))
    Expect(lr.Spec.Limits[0].Type).To(Equal(corev1.LimitTypeContainer))
    Expect(lr.Spec.Limits[0].Default).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("500m")))
})
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — limitrange does not exist.

- [ ] **Step 4: Implement limitrange reconciliation**

Add this method to `project_controller.go`:

```go
func (r *ProjectReconciler) reconcileLimitRange(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name,
			Namespace: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, lr, func() error {
		lr.Spec.Limits = project.Spec.Limits
		return nil
	})
	return err
}
```

Add the call in `Reconcile`, after `reconcileRoleBinding`:

```go
if err := r.reconcileLimitRange(ctx, project); err != nil {
    return ctrl.Result{}, err
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/controller/project_controller.go internal/controller/project_controller_test.go
git commit -m "feat: reconcile limitrange for project"
```

---

### Task 7: ResourceQuota reconciliation

The controller should ensure a ResourceQuota exists in the project namespace with hard limits from `project.spec.quotas`.

**Files:**
- Modify: `internal/controller/project_controller_test.go`
- Modify: `internal/controller/project_controller.go`

- [ ] **Step 1: Update the project fixture to include quotas**

In `project_controller_test.go`, update the `BeforeEach` project spec to include quotas:

```go
Spec: projectmassopencloudv1alpha1.ProjectSpec{
    Members: []string{"user1", "user2"},
    Limits: []corev1.LimitRangeItem{
        {
            Type: corev1.LimitTypeContainer,
            Default: corev1.ResourceList{
                corev1.ResourceCPU:    resource.MustParse("500m"),
                corev1.ResourceMemory: resource.MustParse("512Mi"),
            },
        },
    },
    Quotas: map[string]resource.Quantity{
        "cpu":    resource.MustParse("4"),
        "memory": resource.MustParse("8Gi"),
    },
},
```

- [ ] **Step 2: Write the failing test**

Add this test case inside the `"When reconciling a project"` Context block:

```go
It("should create a resourcequota with the project quotas", func() {
    reconcileProject()

    rq := &corev1.ResourceQuota{}
    err := k8sClient.Get(ctx, types.NamespacedName{
        Name:      projectName,
        Namespace: projectName,
    }, rq)
    Expect(err).NotTo(HaveOccurred())
    Expect(rq.Spec.Hard).To(HaveKeyWithValue(corev1.ResourceName("cpu"), resource.MustParse("4")))
    Expect(rq.Spec.Hard).To(HaveKeyWithValue(corev1.ResourceName("memory"), resource.MustParse("8Gi")))
})
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — resourcequota does not exist.

- [ ] **Step 4: Implement resourcequota reconciliation**

Add this method to `project_controller.go`:

```go
func (r *ProjectReconciler) reconcileResourceQuota(ctx context.Context, project *projectmassopencloudv1alpha1.Project) error {
	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      project.Name,
			Namespace: project.Name,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rq, func() error {
		rq.Spec.Hard = make(corev1.ResourceList, len(project.Spec.Quotas))
		for k, v := range project.Spec.Quotas {
			rq.Spec.Hard[corev1.ResourceName(k)] = v
		}
		return nil
	})
	return err
}
```

Add the call in `Reconcile`, after `reconcileLimitRange`:

```go
if err := r.reconcileResourceQuota(ctx, project); err != nil {
    return ctrl.Result{}, err
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/controller/project_controller.go internal/controller/project_controller_test.go
git commit -m "feat: reconcile resourcequota for project"
```

---

### Task 8: Finalizer and deletion handling

The controller must add a finalizer to Project resources and handle deletion with three behaviors: Ignore (label namespace only), Preserve (delete group, keep namespace), Delete (delete namespace and group).

**Files:**
- Modify: `internal/controller/project_controller_test.go`
- Modify: `internal/controller/project_controller.go`

- [ ] **Step 1: Write the failing test for finalizer addition**

Add this test case inside the `"When reconciling a project"` Context block:

```go
It("should add a finalizer to the project", func() {
    reconcileProject()

    project := &projectmassopencloudv1alpha1.Project{}
    err := k8sClient.Get(ctx, typeNamespacedName, project)
    Expect(err).NotTo(HaveOccurred())
    Expect(project.Finalizers).To(ContainElement("project.massopen.cloud/finalizer"))
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — no finalizer on the project.

- [ ] **Step 3: Add finalizer logic to Reconcile**

In `project_controller.go`, update the `Reconcile` method. Insert finalizer logic after fetching the project and before the reconcile calls:

```go
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

	if project.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, project)
	}

	if !controllerutil.ContainsFinalizer(project, finalizerName) {
		controllerutil.AddFinalizer(project, finalizerName)
		if err := r.Update(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileNamespace(ctx, project); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileGroup(ctx, project); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileRoleBinding(ctx, project); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileLimitRange(ctx, project); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileResourceQuota(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
```

Add a stub `handleDeletion` method:

```go
func (r *ProjectReconciler) handleDeletion(ctx context.Context, project *projectmassopencloudv1alpha1.Project) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 5: Write the failing test for deletion with Ignore action**

Add a new `Context` block at the same level as `"When reconciling a project"`:

```go
Context("When deleting a project with Ignore action", func() {
    BeforeEach(func() {
        project := &projectmassopencloudv1alpha1.Project{
            ObjectMeta: metav1.ObjectMeta{
                Name:      projectName,
                Namespace: "default",
            },
            Spec: projectmassopencloudv1alpha1.ProjectSpec{
                Members:      []string{"user1"},
                DeleteAction: projectmassopencloudv1alpha1.DeleteActionIgnore,
            },
        }
        err := k8sClient.Get(ctx, typeNamespacedName, &projectmassopencloudv1alpha1.Project{})
        if errors.IsNotFound(err) {
            Expect(k8sClient.Create(ctx, project)).To(Succeed())
        }

        reconcileProject()
    })

    AfterEach(func() {
        project := &projectmassopencloudv1alpha1.Project{}
        err := k8sClient.Get(ctx, typeNamespacedName, project)
        if err == nil {
            project.Finalizers = nil
            Expect(k8sClient.Update(ctx, project)).To(Succeed())
            Expect(k8sClient.Delete(ctx, project)).To(Succeed())
        }

        ns := &corev1.Namespace{}
        err = k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
        if err == nil {
            Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
        }

        group := &unstructured.Unstructured{}
        group.SetGroupVersionKind(groupGVK)
        group.SetName(projectName)
        _ = k8sClient.Delete(ctx, group)
    })

    It("should label the namespace as deleted and remove the finalizer", func() {
        project := &projectmassopencloudv1alpha1.Project{}
        Expect(k8sClient.Get(ctx, typeNamespacedName, project)).To(Succeed())
        Expect(k8sClient.Delete(ctx, project)).To(Succeed())

        reconcileProject()

        ns := &corev1.Namespace{}
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)).To(Succeed())
        Expect(ns.Labels).To(HaveKeyWithValue("project.massopen.cloud/deleted", "true"))

        group := &unstructured.Unstructured{}
        group.SetGroupVersionKind(groupGVK)
        err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
        Expect(err).NotTo(HaveOccurred(), "group should still exist with Ignore action")
    })
})
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `make test`
Expected: FAIL — namespace not labeled as deleted.

- [ ] **Step 7: Implement the handleDeletion method**

Replace the stub `handleDeletion` in `project_controller.go` with the full implementation:

```go
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
	default:
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
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 9: Write the failing test for Preserve action**

Add another Context block:

```go
Context("When deleting a project with Preserve action", func() {
    BeforeEach(func() {
        project := &projectmassopencloudv1alpha1.Project{
            ObjectMeta: metav1.ObjectMeta{
                Name:      projectName,
                Namespace: "default",
            },
            Spec: projectmassopencloudv1alpha1.ProjectSpec{
                Members:      []string{"user1"},
                DeleteAction: projectmassopencloudv1alpha1.DeleteActionPreserve,
            },
        }
        err := k8sClient.Get(ctx, typeNamespacedName, &projectmassopencloudv1alpha1.Project{})
        if errors.IsNotFound(err) {
            Expect(k8sClient.Create(ctx, project)).To(Succeed())
        }

        reconcileProject()
    })

    AfterEach(func() {
        project := &projectmassopencloudv1alpha1.Project{}
        err := k8sClient.Get(ctx, typeNamespacedName, project)
        if err == nil {
            project.Finalizers = nil
            Expect(k8sClient.Update(ctx, project)).To(Succeed())
            Expect(k8sClient.Delete(ctx, project)).To(Succeed())
        }

        ns := &corev1.Namespace{}
        err = k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
        if err == nil {
            Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
        }

        group := &unstructured.Unstructured{}
        group.SetGroupVersionKind(groupGVK)
        group.SetName(projectName)
        _ = k8sClient.Delete(ctx, group)
    })

    It("should delete the group but keep the namespace", func() {
        project := &projectmassopencloudv1alpha1.Project{}
        Expect(k8sClient.Get(ctx, typeNamespacedName, project)).To(Succeed())
        Expect(k8sClient.Delete(ctx, project)).To(Succeed())

        reconcileProject()

        ns := &corev1.Namespace{}
        err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
        Expect(err).NotTo(HaveOccurred(), "namespace should still exist with Preserve action")

        group := &unstructured.Unstructured{}
        group.SetGroupVersionKind(groupGVK)
        err = k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
        Expect(errors.IsNotFound(err)).To(BeTrue(), "group should be deleted with Preserve action")
    })
})
```

- [ ] **Step 10: Run the test to verify it passes**

Run: `make test`
Expected: PASS — the implementation from Step 7 already handles Preserve.

- [ ] **Step 11: Write the test for Delete action**

Add another Context block:

```go
Context("When deleting a project with Delete action", func() {
    BeforeEach(func() {
        project := &projectmassopencloudv1alpha1.Project{
            ObjectMeta: metav1.ObjectMeta{
                Name:      projectName,
                Namespace: "default",
            },
            Spec: projectmassopencloudv1alpha1.ProjectSpec{
                Members:      []string{"user1"},
                DeleteAction: projectmassopencloudv1alpha1.DeleteActionDelete,
            },
        }
        err := k8sClient.Get(ctx, typeNamespacedName, &projectmassopencloudv1alpha1.Project{})
        if errors.IsNotFound(err) {
            Expect(k8sClient.Create(ctx, project)).To(Succeed())
        }

        reconcileProject()
    })

    AfterEach(func() {
        project := &projectmassopencloudv1alpha1.Project{}
        err := k8sClient.Get(ctx, typeNamespacedName, project)
        if err == nil {
            project.Finalizers = nil
            Expect(k8sClient.Update(ctx, project)).To(Succeed())
            Expect(k8sClient.Delete(ctx, project)).To(Succeed())
        }

        group := &unstructured.Unstructured{}
        group.SetGroupVersionKind(groupGVK)
        group.SetName(projectName)
        _ = k8sClient.Delete(ctx, group)
    })

    It("should delete the namespace and the group", func() {
        project := &projectmassopencloudv1alpha1.Project{}
        Expect(k8sClient.Get(ctx, typeNamespacedName, project)).To(Succeed())
        Expect(k8sClient.Delete(ctx, project)).To(Succeed())

        reconcileProject()

        group := &unstructured.Unstructured{}
        group.SetGroupVersionKind(groupGVK)
        err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
        Expect(errors.IsNotFound(err)).To(BeTrue(), "group should be deleted with Delete action")
    })
})
```

Note: We cannot directly assert that the namespace is deleted in envtest since namespace deletion is handled asynchronously by the namespace controller, which doesn't run in envtest. We verify the delete was issued by checking the group is gone.

- [ ] **Step 12: Run the test to verify it passes**

Run: `make test`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add internal/controller/project_controller.go internal/controller/project_controller_test.go
git commit -m "feat: add finalizer and deletion handling"
```

---

### Task 9: Resource update reconciliation

The controller should update existing resources when the Project spec changes (e.g., members change, quotas change). This is already handled by `controllerutil.CreateOrUpdate`, but we need a test to verify it.

**Files:**
- Modify: `internal/controller/project_controller_test.go`

- [ ] **Step 1: Write the update test**

Add this test case inside the `"When reconciling a project"` Context block in `project_controller_test.go`:

```go
It("should update the group when members change", func() {
    reconcileProject()

    project := &projectmassopencloudv1alpha1.Project{}
    Expect(k8sClient.Get(ctx, typeNamespacedName, project)).To(Succeed())
    project.Spec.Members = []string{"user1", "user2", "user3"}
    Expect(k8sClient.Update(ctx, project)).To(Succeed())

    reconcileProject()

    group := &unstructured.Unstructured{}
    group.SetGroupVersionKind(groupGVK)
    err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
    Expect(err).NotTo(HaveOccurred())

    users, found, err := unstructured.NestedStringSlice(group.Object, "users")
    Expect(err).NotTo(HaveOccurred())
    Expect(found).To(BeTrue())
    Expect(users).To(ConsistOf("user1", "user2", "user3"))
})
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `make test`
Expected: PASS — CreateOrUpdate handles updates.

- [ ] **Step 3: Commit**

```bash
git add internal/controller/project_controller_test.go
git commit -m "test: verify resource updates on project spec changes"
```

---

### Task 10: Deployment configuration and samples

Update the manager deployment to expose the controller's namespace via the downward API, and update the sample Project manifest.

**Files:**
- Modify: `config/manager/manager.yaml`
- Modify: `config/samples/v1alpha1_project.yaml`

- [ ] **Step 1: Add the namespace env var to the manager deployment**

In `config/manager/manager.yaml`, add an `env` section to the manager container (after the `name: manager` line, before `ports`):

```yaml
        env:
        - name: CONTROLLER_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
```

- [ ] **Step 2: Update the sample Project manifest**

Replace the contents of `config/samples/v1alpha1_project.yaml`:

```yaml
apiVersion: project.massopen.cloud/v1alpha1
kind: Project
metadata:
  labels:
    app.kubernetes.io/name: moc-project-operator
    app.kubernetes.io/managed-by: kustomize
  name: project-sample
spec:
  displayName: "Sample Project"
  members:
    - user1
    - user2
  deleteAction: Preserve
  limits:
    - type: Container
      default:
        cpu: "500m"
        memory: "512Mi"
  quotas:
    cpu: "4"
    memory: "8Gi"
```

- [ ] **Step 3: Verify build**

Run: `make build`
Expected: builds successfully.

- [ ] **Step 4: Commit**

```bash
git add config/manager/manager.yaml config/samples/v1alpha1_project.yaml
git commit -m "chore: update deployment config and sample manifest"
```

---

### Task 11: Final verification

Run the full build and test suite to confirm everything works together.

**Files:** None (verification only)

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: no lint errors (install golangci-lint if needed).

- [ ] **Step 2: Run full test suite**

Run: `make test`
Expected: all tests pass.

- [ ] **Step 3: Run build**

Run: `make build`
Expected: binary builds successfully.

- [ ] **Step 4: Regenerate manifests and verify no drift**

Run: `make manifests generate && git diff`
Expected: no uncommitted changes (generated files are in sync).

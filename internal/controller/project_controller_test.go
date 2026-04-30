package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	})
})

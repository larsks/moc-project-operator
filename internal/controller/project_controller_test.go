package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

	reconcileProject := func() {
		r := &ProjectReconciler{
			Client:              k8sClient,
			Scheme:              k8sClient.Scheme(),
			ControllerNamespace: "default",
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	}

	Context("When reconciling a project", func() {
		BeforeEach(func() {
			project := &projectmassopencloudv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: "default",
				},
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

		It("should create a namespace with the project name label", func() {
			reconcileProject()

			ns := &corev1.Namespace{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Labels).To(HaveKeyWithValue("project.massopen.cloud/name", projectName))
		})

		It("should remove the deleted label from a namespace", func() {
			reconcileProject()

			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)).To(Succeed())
			ns.Labels[labelProjectDeleted] = "true"
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			reconcileProject()

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)).To(Succeed())
			Expect(ns.Labels).NotTo(HaveKey(labelProjectDeleted))
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

		It("should add a finalizer to the project", func() {
			reconcileProject()

			project := &projectmassopencloudv1alpha1.Project{}
			err := k8sClient.Get(ctx, typeNamespacedName, project)
			Expect(err).NotTo(HaveOccurred())
			Expect(project.Finalizers).To(ContainElement("project.massopen.cloud/finalizer"))
		})

		It("should re-create a child resource if it is deleted", func() {
			reconcileProject()

			group := &unstructured.Unstructured{}
			group.SetGroupVersionKind(groupGVK)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)).To(Succeed())
			Expect(k8sClient.Delete(ctx, group)).To(Succeed())

			err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			reconcileProject()

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)).To(Succeed())
			users, found, err := unstructured.NestedStringSlice(group.Object, "users")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(users).To(ConsistOf("user1", "user2"))
		})

		It("should label child resources with the project name", func() {
			reconcileProject()

			group := &unstructured.Unstructured{}
			group.SetGroupVersionKind(groupGVK)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)).To(Succeed())
			Expect(group.GetLabels()).To(HaveKeyWithValue(labelProjectName, projectName))

			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: projectName}, rb)).To(Succeed())
			Expect(rb.Labels).To(HaveKeyWithValue(labelProjectName, projectName))

			lr := &corev1.LimitRange{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: projectName}, lr)).To(Succeed())
			Expect(lr.Labels).To(HaveKeyWithValue(labelProjectName, projectName))

			rq := &corev1.ResourceQuota{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: projectName}, rq)).To(Succeed())
			Expect(rq.Labels).To(HaveKeyWithValue(labelProjectName, projectName))
		})

		It("should set phase to Ready after successful reconciliation", func() {
			reconcileProject()

			project := &projectmassopencloudv1alpha1.Project{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, project)).To(Succeed())
			Expect(project.Status.Phase).To(Equal(projectmassopencloudv1alpha1.PhaseReady))
		})

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
	})

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

	Context("When deleting a project with no deleteAction set", func() {
		BeforeEach(func() {
			project := &projectmassopencloudv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{
					Name:      projectName,
					Namespace: "default",
				},
				Spec: projectmassopencloudv1alpha1.ProjectSpec{
					Members: []string{"user1"},
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

		It("should default to Preserve behavior (delete group, keep namespace)", func() {
			project := &projectmassopencloudv1alpha1.Project{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, project)).To(Succeed())
			Expect(k8sClient.Delete(ctx, project)).To(Succeed())
			reconcileProject()

			ns := &corev1.Namespace{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, ns)
			Expect(err).NotTo(HaveOccurred(), "namespace should still exist with default action")

			group := &unstructured.Unstructured{}
			group.SetGroupVersionKind(groupGVK)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: projectName}, group)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "group should be deleted with default action (Preserve)")
		})
	})
})

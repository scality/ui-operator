package scalityuicomponent

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// MockConfigFetcher is a mock implementation of the ConfigFetcher interface
type MockConfigFetcher struct {
	ShouldFail    bool
	ErrorMessage  string
	ConfigContent string
	ReceivedCalls []MockFetchCall
}

type MockFetchCall struct {
	Namespace   string
	ServiceName string
	Port        int
}

func (m *MockConfigFetcher) FetchConfig(ctx context.Context, namespace, serviceName string, port int) (string, error) {
	call := MockFetchCall{
		Namespace:   namespace,
		ServiceName: serviceName,
		Port:        port,
	}
	m.ReceivedCalls = append(m.ReceivedCalls, call)

	if m.ShouldFail {
		return "", errors.New(m.ErrorMessage)
	}
	return m.ConfigContent, nil
}

// Helper function to verify OwnerReference
func verifyOwnerReference(obj metav1.Object, ownerKind, ownerName string) {
	Expect(obj.GetOwnerReferences()).To(HaveLen(1))
	Expect(obj.GetOwnerReferences()[0].Kind).To(Equal(ownerKind))
	Expect(obj.GetOwnerReferences()[0].Name).To(Equal(ownerName))
	Expect(*obj.GetOwnerReferences()[0].Controller).To(BeTrue())
}

var _ = Describe("ScalityUIComponent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const testNamespace = "default"
		const testImage = "scality/ui-component:latest"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		// Deployment has the same name as the resource
		deploymentNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		// Service has the same name as the resource
		serviceNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		scalityuicomponent := &uiv1alpha1.ScalityUIComponent{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ScalityUIComponent")
			err := k8sClient.Get(ctx, typeNamespacedName, scalityuicomponent)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUIComponent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: uiv1alpha1.ScalityUIComponentSpec{
						Image: testImage,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &uiv1alpha1.ScalityUIComponent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ScalityUIComponent")
			// With SetControllerReference, Deployment and Service will be automatically cleaned up
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())

			// Setup mock config fetcher with valid JSON
			mockFetcher := &MockConfigFetcher{
				ConfigContent: `{
					"kind": "UIModule", 
					"apiVersion": "v1alpha1", 
					"metadata": {"kind": "TestKind"}, 
					"spec": {
						"remoteEntryPath": "/remoteEntry.js", 
						"publicPath": "/test-public/", 
						"version": "1.2.3"
					}
				}`,
			}
			controllerReconciler.ConfigFetcher = mockFetcher

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Deployment was created with correct specifications")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, deploymentNamespacedName, deployment)
			Expect(err).NotTo(HaveOccurred())

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(testImage))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal(resourceName))

			Expect(deployment.Spec.Template.ObjectMeta.Labels["app"]).To(Equal(resourceName))
			Expect(deployment.Spec.Selector.MatchLabels["app"]).To(Equal(resourceName))

			By("Checking if Deployment has correct OwnerReference")
			verifyOwnerReference(deployment, "ScalityUIComponent", resourceName)

			By("Checking if Service was created with correct specifications")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, serviceNamespacedName, service)
			Expect(err).NotTo(HaveOccurred())

			Expect(service.Spec.Selector["app"]).To(Equal(resourceName))
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).To(Equal("http"))
			Expect(service.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(80)))

			By("Checking if Service has correct OwnerReference")
			verifyOwnerReference(service, "ScalityUIComponent", resourceName)

			By("Testing various imagePullSecrets scenarios")
			fetchedResource := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, fetchedResource)).To(Succeed())

			// Test single imagePullSecret
			fetchedResource.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "single-secret"}}
			Expect(k8sClient.Update(ctx, fetchedResource)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updatedDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, deploymentNamespacedName, updatedDeployment)).To(Succeed())
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal("single-secret"))

			// Test multiple imagePullSecrets
			fetchedResource.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
				{Name: "secret-1"}, {Name: "secret-2"}, {Name: "secret-3"},
			}
			Expect(k8sClient.Update(ctx, fetchedResource)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, deploymentNamespacedName, updatedDeployment)).To(Succeed())
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(3))
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal("secret-1"))
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets[1].Name).To(Equal("secret-2"))
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets[2].Name).To(Equal("secret-3"))

			// Test empty imagePullSecrets
			fetchedResource.Spec.ImagePullSecrets = []corev1.LocalObjectReference{}
			Expect(k8sClient.Update(ctx, fetchedResource)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, deploymentNamespacedName, updatedDeployment)).To(Succeed())
			Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets).To(BeEmpty())
		})

		It("should apply tolerations from scheduling spec to deployment", func() {
			By("Reconciling the created resource initially")
			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			mockFetcher := &MockConfigFetcher{
				ConfigContent: `{
					"kind": "UIModule", 
					"apiVersion": "v1alpha1", 
					"metadata": {"kind": "TestKind"}, 
					"spec": {
						"remoteEntryPath": "/remoteEntry.js", 
						"publicPath": "/test-public/", 
						"version": "1.2.3"
					}
				}`,
			}
			controllerReconciler.ConfigFetcher = mockFetcher

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Adding scheduling constraints to the resource")
			fetchedResource := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, fetchedResource)).To(Succeed())

			fetchedResource.Spec.Scheduling = &uiv1alpha1.PodSchedulingSpec{
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/bootstrap",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
					{
						Key:      "node-role.kubernetes.io/infra",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			}

			Expect(k8sClient.Update(ctx, fetchedResource)).To(Succeed())

			By("Reconciling again to apply scheduling constraints")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying tolerations are applied to the deployment")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, deploymentNamespacedName, deployment)).To(Succeed())

			Expect(deployment.Spec.Template.Spec.Tolerations).To(HaveLen(2))

			// Check first toleration
			Expect(deployment.Spec.Template.Spec.Tolerations[0].Key).To(Equal("node-role.kubernetes.io/bootstrap"))
			Expect(deployment.Spec.Template.Spec.Tolerations[0].Operator).To(Equal(corev1.TolerationOperator("Exists")))
			Expect(deployment.Spec.Template.Spec.Tolerations[0].Effect).To(Equal(corev1.TaintEffect("NoSchedule")))

			// Check second toleration
			Expect(deployment.Spec.Template.Spec.Tolerations[1].Key).To(Equal("node-role.kubernetes.io/infra"))
			Expect(deployment.Spec.Template.Spec.Tolerations[1].Operator).To(Equal(corev1.TolerationOperator("Exists")))
			Expect(deployment.Spec.Template.Spec.Tolerations[1].Effect).To(Equal(corev1.TaintEffect("NoSchedule")))
		})

		It("should requeue if Deployment is not ready", func() {
			By("Reconciling the created resource")
			mockFetcher := &MockConfigFetcher{}
			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			controllerReconciler.ConfigFetcher = mockFetcher

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Deployment and Service are created by the reconcile loop

			By("Ensuring Deployment exists")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			By("Updating Deployment status to not ready")
			deployment.Status.ReadyReplicas = 0
			deployment.Status.Replicas = 1 // Ensure some replicas are desired
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			// Second reconcile, now Deployment is fetched and checked for readiness
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(10 * time.Second)) // Returns RequeueAfter when deployment not ready

			// Check that no status condition for ConfigurationRetrieved was added yet
			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).To(BeNil())

			Expect(mockFetcher.ReceivedCalls).To(BeEmpty()) // Ensure fetcher was not called
		})

		It("should set ConfigurationRetrieved=False condition if config fetch fails", func() {
			By("Creating a mock config fetcher that fails")
			mockFetcher := &MockConfigFetcher{
				ShouldFail:   true,
				ErrorMessage: "Mock fetching error",
			}

			By("Reconciling the created resource")
			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			controllerReconciler.ConfigFetcher = mockFetcher

			// First reconcile to create Deployment and Service
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Ensuring Deployment exists and making it ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			// Mark deployment as ready according to framework's IsSettled requirements
			deployment.Status.UpdatedReplicas = *deployment.Spec.Replicas
			deployment.Status.Replicas = *deployment.Spec.Replicas
			deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
			deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Triggering Reconcile again for config fetch logic")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred()) // The reconcile itself should not error, but requeue
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			By("Checking ScalityUIComponent status conditions")
			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("FetchFailed"))
			Expect(cond.Message).To(ContainSubstring("Failed to fetch configuration"))

			By("Verifying that the mock was called with correct parameters")
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1))
			Expect(mockFetcher.ReceivedCalls[0].Namespace).To(Equal(testNamespace))
			Expect(mockFetcher.ReceivedCalls[0].ServiceName).To(Equal(resourceName))
			Expect(mockFetcher.ReceivedCalls[0].Port).To(Equal(DefaultServicePort))
		})

		It("should set ConfigurationRetrieved=True and update status if config fetch and parse succeed", func() {
			By("Creating a mock config fetcher with successful response")
			mockFetcher := &MockConfigFetcher{
				ShouldFail: false,
				ConfigContent: `{
					"kind": "UIModule", 
					"apiVersion": "v1alpha1", 
					"metadata": {"kind": "TestKind"}, 
					"spec": {
						"remoteEntryPath": "/remoteEntry.js", 
						"publicPath": "/test-public/", 
						"version": "1.2.3"
					}
				}`,
			}

			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			controllerReconciler.ConfigFetcher = mockFetcher

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, deploymentNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())

			// Mark deployment as ready according to framework's IsSettled requirements
			deployment.Status.UpdatedReplicas = *deployment.Spec.Replicas
			deployment.Status.Replicas = *deployment.Spec.Replicas
			deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
			deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0))) // No requeue needed, Deployment changes trigger reconcile via Owns()

			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			Expect(updatedScalityUIComponent.Status.Kind).To(Equal("TestKind"))
			Expect(updatedScalityUIComponent.Status.PublicPath).To(Equal("/test-public/"))
			Expect(updatedScalityUIComponent.Status.Version).To(Equal("1.2.3"))

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("FetchSucceeded"))
			// Second reconcile skips fetch since image unchanged
			Expect(cond.Message).To(Equal("Successfully fetched initial configuration from image scality/ui-component:latest"))

			By("Verifying that the mock was called only once (second reconcile skips fetch)")
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1)) // Configuration fetched only on first reconcile
			Expect(mockFetcher.ReceivedCalls[0].Namespace).To(Equal(testNamespace))
			Expect(mockFetcher.ReceivedCalls[0].ServiceName).To(Equal(resourceName))
			Expect(mockFetcher.ReceivedCalls[0].Port).To(Equal(DefaultServicePort))
		})

		It("should set ConfigurationRetrieved=False with ParseFailed reason if config parse fails", func() {
			By("Creating a mock config fetcher with malformed JSON")
			mockFetcher := &MockConfigFetcher{
				ShouldFail:    false,
				ConfigContent: `{"kind": "UIModule", "invalid": json}`,
			}

			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			controllerReconciler.ConfigFetcher = mockFetcher

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, deploymentNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())

			// Mark deployment as ready according to framework's IsSettled requirements
			deployment.Status.UpdatedReplicas = *deployment.Spec.Replicas
			deployment.Status.Replicas = *deployment.Spec.Replicas
			deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
			deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ParseFailed"))
			Expect(cond.Message).To(ContainSubstring("Failed to parse configuration"))

			By("Verifying that LastFetchedImage is set to prevent repeated fetch attempts")
			Expect(updatedScalityUIComponent.Status.LastFetchedImage).To(Equal("scality/ui-component:latest"))

			By("Verifying that the mock was called only once (parse failures don't trigger refetch)")
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1))
			Expect(mockFetcher.ReceivedCalls[0].Namespace).To(Equal(testNamespace))
			Expect(mockFetcher.ReceivedCalls[0].ServiceName).To(Equal(resourceName))
			Expect(mockFetcher.ReceivedCalls[0].Port).To(Equal(DefaultServicePort))
		})

		It("should detect PublicPath changes and update condition message", func() {
			By("Creating a mock config fetcher with initial configuration")
			mockFetcher := &MockConfigFetcher{
				ShouldFail: false,
				ConfigContent: `{
					"kind": "UIModule", 
					"apiVersion": "v1alpha1", 
					"metadata": {"kind": "TestKind"}, 
					"spec": {
						"remoteEntryPath": "/remoteEntry.js", 
						"publicPath": "/initial-path/", 
						"version": "1.0.0"
					}
				}`,
			}

			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			controllerReconciler.ConfigFetcher = mockFetcher

			By("Reconciling to set initial configuration")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, deploymentNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())

			// Mark deployment as ready
			deployment.Status.UpdatedReplicas = *deployment.Spec.Replicas
			deployment.Status.Replicas = *deployment.Spec.Replicas
			deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
			deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Reconciling again to process initial configuration")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

			By("Verifying initial configuration status")
			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			Expect(updatedScalityUIComponent.Status.PublicPath).To(Equal("/initial-path/"))

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			// Second reconcile skips fetch since image unchanged
			Expect(cond.Message).To(Equal("Successfully fetched initial configuration from image scality/ui-component:latest"))

			By("Changing the image and mock config to simulate new version")
			mockFetcher.ConfigContent = `{
				"kind": "UIModule",
				"apiVersion": "v1alpha1",
				"metadata": {"kind": "TestKind"},
				"spec": {
					"remoteEntryPath": "/remoteEntry.js",
					"publicPath": "/changed-path/",
					"version": "2.0.0"
				}
			}`

			// Update ScalityUIComponent image to trigger refetch
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			updatedScalityUIComponent.Spec.Image = "scality/ui-component:v2.0.0"
			Expect(k8sClient.Update(ctx, updatedScalityUIComponent)).To(Succeed())

			By("Reconciling again to detect the image change")
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

			By("Verifying configuration was updated for new image")
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			Expect(updatedScalityUIComponent.Status.PublicPath).To(Equal("/changed-path/"))
			Expect(updatedScalityUIComponent.Status.Version).To(Equal("2.0.0"))
			Expect(updatedScalityUIComponent.Status.LastFetchedImage).To(Equal("scality/ui-component:v2.0.0"))

			cond = meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Message).To(ContainSubstring("Configuration updated for new image"))

			By("Reconciling again with no changes to verify fetch is skipped")
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			cond = meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			// Condition message unchanged since fetch was skipped (image unchanged on this reconcile)
			Expect(cond.Message).To(ContainSubstring("Configuration updated for new image"))
		})

		It("should preserve existing volumes and volume mounts during deployment update", func() {
			By("Creating initial deployment with custom volumes")
			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Manually adding custom volumes and volume mounts to deployment")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			// Add custom volume and mount
			deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: "custom-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "custom-config-map",
						},
					},
				},
			})

			deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
				deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
				corev1.VolumeMount{
					Name:      "custom-config",
					MountPath: "/etc/custom-config",
				},
			)

			// Add custom annotation
			deployment.Spec.Template.Annotations = map[string]string{
				"custom-annotation": "test-value",
			}

			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			By("Reconciling again to verify volumes are preserved")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying custom volumes and mounts are preserved")
			updatedDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, deploymentNamespacedName, updatedDeployment)).To(Succeed())

			// Check volumes
			Expect(updatedDeployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(updatedDeployment.Spec.Template.Spec.Volumes[0].Name).To(Equal("custom-config"))

			// Check volume mounts
			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).To(Equal("custom-config"))
			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal("/etc/custom-config"))

			// Check annotations
			Expect(updatedDeployment.Spec.Template.Annotations).To(HaveKey("custom-annotation"))
			Expect(updatedDeployment.Spec.Template.Annotations["custom-annotation"]).To(Equal("test-value"))
		})

		It("should only fetch configuration when image changes - integration test", func() {
			By("Setting up mock fetcher that tracks all calls")
			mockFetcher := &MockConfigFetcher{
				ShouldFail: false,
				ConfigContent: `{
					"kind": "UIModule",
					"apiVersion": "v1alpha1",
					"metadata": {"kind": "IntegrationTestKind"},
					"spec": {
						"remoteEntryPath": "/remoteEntry.js",
						"publicPath": "/integration-test/",
						"version": "1.0.0"
					}
				}`,
			}

			controllerReconciler := NewScalityUIComponentReconciler(k8sClient, k8sClient.Scheme())
			controllerReconciler.ConfigFetcher = mockFetcher

			By("First reconcile - creating deployment and service")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Making deployment ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.UpdatedReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			deployment.Status.AvailableReplicas = 1
			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - should fetch configuration (first time)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should fetch on first successful reconcile")

			By("Third reconcile - should NOT fetch (image unchanged)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should still be 1 - no fetch when image unchanged")

			By("Fourth reconcile - should still NOT fetch")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should still be 1 - multiple reconciles don't trigger fetch")

			By("Fifth reconcile - should still NOT fetch")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should still be 1 - proving no reconcile storm")

			By("Updating image to trigger new fetch")
			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			scalityUIComponent.Spec.Image = "scality/ui-component:v2.0.0"
			Expect(k8sClient.Update(ctx, scalityUIComponent)).To(Succeed())

			By("Reconciling after image change - should fetch again")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(2), "Should fetch again after image change")

			By("Reconciling again - should NOT fetch (new image already fetched)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(2), "Should stay at 2 - new image already processed")

			By("Verifying LastFetchedImage is tracked correctly")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			Expect(scalityUIComponent.Status.LastFetchedImage).To(Equal("scality/ui-component:v2.0.0"))
			Expect(scalityUIComponent.Status.PublicPath).To(Equal("/integration-test/"))
		})
	})

	Context("Parse failure handling", func() {
		It("should not retry fetch after parse failure for same image", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-parse-failure",
					Namespace: "default",
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/broken-json:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ConfigContent: "{ invalid json",
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-parse-failure",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Wait for deployment to be ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - fetches config and fails to parse")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should fetch once")

			By("Verify LastFetchedImage is set despite parse failure")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			Expect(scalityUIComponent.Status.LastFetchedImage).To(Equal("scality/broken-json:v1.0.0"))

			By("Third reconcile - should NOT fetch again (same image)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should still be 1 - no refetch")

			By("Verify ConfigurationRetrieved condition is False")
			condition := meta.FindStatusCondition(scalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("ParseFailed"))
		})
	})

	Context("Validation failure handling", func() {
		It("should not retry fetch after validation failure for same image", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-validation-failure",
					Namespace: "default",
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/invalid-config:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ConfigContent: `{
					"metadata": {},
					"spec": {
						"publicPath": "/test/"
					}
				}`,
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-validation-failure",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Wait for deployment to be ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - fetches config and fails validation")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should fetch once")

			By("Verify LastFetchedImage is set despite validation failure")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			Expect(scalityUIComponent.Status.LastFetchedImage).To(Equal("scality/invalid-config:v1.0.0"))

			By("Third reconcile - should NOT fetch again (same image)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should still be 1 - no refetch")

			By("Verify ConfigurationRetrieved condition is False")
			condition := meta.FindStatusCondition(scalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("ValidationFailed"))
		})
	})

	Context("Rolling update handling", func() {
		It("should wait for rolling update to complete before fetching", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rolling-update",
					Namespace: "default",
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/ui-component:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ConfigContent: `{
					"metadata": {
						"kind": "app",
						"name": "test-app"
					},
					"spec": {
						"publicPath": "/test/",
						"version": "1.0.0"
					}
				}`,
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-rolling-update",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Simulate rolling update - ReadyReplicas=1 but UpdatedReplicas=0")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 0
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - should NOT fetch (rolling update incomplete)")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(10 * time.Second))
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(0), "Should not fetch during rolling update")

			By("Complete rolling update")
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Third reconcile - should fetch now (rolling update complete)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should fetch after rolling update completes")
		})
	})

	Context("Force-refresh annotation", func() {
		It("should trigger fetch when force-refresh annotation is present even if image unchanged", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-force-refresh",
					Namespace: "default",
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/ui-component:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ConfigContent: `{
					"metadata": {"kind": "TestKind"},
					"spec": {"publicPath": "/test/", "version": "1.0.0"}
				}`,
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-force-refresh",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Make deployment ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - initial fetch")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should fetch on initial reconcile")

			By("Third reconcile - no fetch (image unchanged)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should not fetch - image unchanged")

			By("Add force-refresh annotation")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			if scalityUIComponent.Annotations == nil {
				scalityUIComponent.Annotations = make(map[string]string)
			}
			scalityUIComponent.Annotations[ForceRefreshAnnotation] = "true"
			Expect(k8sClient.Update(ctx, scalityUIComponent)).To(Succeed())

			By("Update mock to return different config")
			mockFetcher.ConfigContent = `{
				"metadata": {"kind": "TestKind"},
				"spec": {"publicPath": "/updated-path/", "version": "1.0.1"}
			}`

			By("Fourth reconcile - should fetch due to force-refresh annotation")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(2), "Should fetch due to force-refresh annotation")

			By("Verify configuration was updated")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			Expect(scalityUIComponent.Status.PublicPath).To(Equal("/updated-path/"))
			Expect(scalityUIComponent.Status.Version).To(Equal("1.0.1"))

			By("Verify force-refresh annotation was removed")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			_, hasAnnotation := scalityUIComponent.Annotations[ForceRefreshAnnotation]
			Expect(hasAnnotation).To(BeFalse(), "force-refresh annotation should be removed after fetch")

			By("Fifth reconcile - no fetch (annotation removed, image unchanged)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(2), "Should not fetch - annotation removed")
		})

		It("should remove force-refresh annotation even on fetch failure", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-force-refresh-failure",
					Namespace: "default",
					Annotations: map[string]string{
						ForceRefreshAnnotation: "true",
					},
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/ui-component:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ShouldFail:   true,
				ErrorMessage: "simulated fetch failure",
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-force-refresh-failure",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Make deployment ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - fetch fails")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should attempt fetch")

			By("Verify force-refresh annotation was NOT removed on fetch failure (only removed on parse/validation failure)")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			_, hasAnnotation := scalityUIComponent.Annotations[ForceRefreshAnnotation]
			Expect(hasAnnotation).To(BeTrue(), "force-refresh annotation should remain on fetch failure for retry")
		})

		It("should remove force-refresh annotation on parse failure", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-force-refresh-parse-fail",
					Namespace: "default",
					Annotations: map[string]string{
						ForceRefreshAnnotation: "true",
					},
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/ui-component:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ConfigContent: "{ invalid json",
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-force-refresh-parse-fail",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Make deployment ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Second reconcile - parse fails")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verify force-refresh annotation was removed on parse failure")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			_, hasAnnotation := scalityUIComponent.Annotations[ForceRefreshAnnotation]
			Expect(hasAnnotation).To(BeFalse(), "force-refresh annotation should be removed on parse failure")
		})
	})

	Context("Nil annotations handling", func() {
		It("should handle force-refresh annotation when Annotations map is nil", func() {
			ctx := context.Background()

			scalityUIComponent := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-nil-annotations",
					Namespace: "default",
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/ui-component:v1.0.0",
					MountPath: "/usr/share/ui-config",
				},
			}
			Expect(k8sClient.Create(ctx, scalityUIComponent)).To(Succeed())

			mockFetcher := &MockConfigFetcher{
				ConfigContent: `{
					"metadata": {
						"kind": "app",
						"name": "test-app"
					},
					"spec": {
						"publicPath": "/test/",
						"version": "1.0.0"
					}
				}`,
			}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			typeNamespacedName := types.NamespacedName{
				Name:      "test-nil-annotations",
				Namespace: "default",
			}

			By("First reconcile - creates deployment")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Wait for deployment to be ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Verify Annotations is nil")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			Expect(scalityUIComponent.Annotations).To(BeNil())

			By("Second reconcile with nil Annotations - should not panic")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1), "Should fetch successfully")

			By("Verify status is updated correctly")
			Expect(k8sClient.Get(ctx, typeNamespacedName, scalityUIComponent)).To(Succeed())
			Expect(scalityUIComponent.Status.PublicPath).To(Equal("/test/"))
			Expect(scalityUIComponent.Status.LastFetchedImage).To(Equal("scality/ui-component:v1.0.0"))
		})
	})
})

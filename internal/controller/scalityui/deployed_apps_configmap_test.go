package scalityui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"github.com/scality/ui-operator/internal/controller/scalityuicomponent"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Regression for the partial-write race against kubelet subPath mounts.
//
// Scenario:
//
//	T0  reducer runs, all components ready -> writes full ConfigMap
//	T1  one component's ConfigurationRetrieved flips to False (cache
//	    flicker on operator restart, or status reset by the
//	    ScalityUIComponent reconciler during image re-validation)
//	T2  reducer runs again -> without the readiness gate it would write
//	    a partial ConfigMap missing that component
//	T3  kubelet on the node materialises subPath -> reads the partial
//	    ConfigMap -> the mounted file is now permanently partial
//	    (subPath does not propagate later ConfigMap updates)
//	T4  component flips back to True; ConfigMap recovers but the pod
//	    keeps serving the stale partial file forever
//
// envtest has no kubelet, so T3 is modelled by a helper that snapshots
// the ConfigMap the way kubelet would. The gate must hold off the write
// at T2 so the snapshot stays whole.
var _ = Describe("deployed-ui-apps partial-write race", func() {
	const uiName = "shell-ui-cp"

	specs := []struct {
		ns, exposerName, componentName, basePath, url string
	}{
		{"artesca-ui", "identity-ui-exposer", "identity-ui", "/identity", "/identity-ui"},
		{"artesca-ui", "artesca-base-ui-exposer", "artesca-base-ui", "", "/artesca"},
		{"metalk8s-ui-ns", "metalk8s-ui-exposer", "metalk8s-ui", "/platform", "/metalk8s"},
		{"xcore", "xcore-ui-exposer", "xcore-ui", "/storage", "/xcore"},
		{"zenko", "zenko-ui-exposer", "zenko-ui", "/data", "/zenko"},
	}

	clusterScopedName := types.NamespacedName{Name: uiName}

	BeforeEach(func() {
		ctx := context.Background()
		for _, s := range specs {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: s.ns}}
			err := k8sClient.Create(ctx, ns)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}
		sui := &uiv1alpha1.ScalityUI{
			ObjectMeta: metav1.ObjectMeta{Name: uiName},
			Spec: uiv1alpha1.ScalityUISpec{
				Image:       "shell-ui:test",
				ProductName: "Test",
			},
		}
		Expect(k8sClient.Create(ctx, sui)).To(Succeed())

		for _, s := range specs {
			comp := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{Name: s.componentName, Namespace: s.ns},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "comp:test",
					MountPath: "/etc/runtime",
				},
			}
			Expect(k8sClient.Create(ctx, comp)).To(Succeed())

			comp.Status.PublicPath = s.url
			comp.Status.Kind = s.componentName
			meta.SetStatusCondition(&comp.Status.Conditions, metav1.Condition{
				Type:    scalityuicomponent.ConditionTypeConfigurationRetrieved,
				Status:  metav1.ConditionTrue,
				Reason:  "FetchSucceeded",
				Message: "ok",
			})
			Expect(k8sClient.Status().Update(ctx, comp)).To(Succeed())

			exp := &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{Name: s.exposerName, Namespace: s.ns},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: s.componentName,
					AppHistoryBasePath: s.basePath,
				},
			}
			Expect(k8sClient.Create(ctx, exp)).To(Succeed())
		}
	})

	AfterEach(func() {
		ctx := context.Background()
		cleanupTestResources(ctx, clusterScopedName)
		// Delete resources inside the namespaces but leave the namespaces
		// themselves alone: envtest has no namespace controller, so deletion
		// hangs in Terminating and prevents the next BeforeEach from
		// re-creating CRs in those namespaces.
		for _, s := range specs {
			_ = k8sClient.Delete(ctx, &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{Name: s.exposerName, Namespace: s.ns},
			})
			_ = k8sClient.Delete(ctx, &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{Name: s.componentName, Namespace: s.ns},
			})
		}
	})

	flickerMetalk8sCondition := func(ctx context.Context, status metav1.ConditionStatus, reason, message string) {
		comp := &uiv1alpha1.ScalityUIComponent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "metalk8s-ui", Namespace: "metalk8s-ui-ns"}, comp)).To(Succeed())
		meta.SetStatusCondition(&comp.Status.Conditions, metav1.Condition{
			Type:    scalityuicomponent.ConditionTypeConfigurationRetrieved,
			Status:  status,
			Reason:  reason,
			Message: message,
		})
		Expect(k8sClient.Status().Update(ctx, comp)).To(Succeed())
	}

	cmName := types.NamespacedName{Name: uiName + "-deployed-ui-apps", Namespace: getOperatorNamespace()}

	It("holds off writing a partial ConfigMap when a component condition flickers", func() {
		ctx := context.Background()
		reconciler := NewScalityUIReconcilerForTest(k8sClient, k8sClient.Scheme())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		apps5 := decodeApps(cm.Data["deployed-ui-apps.json"])
		Expect(apps5).To(HaveLen(5))
		Expect(appNames(apps5)).To(ContainElement("metalk8s-ui"))
		hash5 := cm.Annotations[deployedAppsHashAnnotation]
		Expect(hash5).NotTo(BeEmpty(), "deployed-apps-hash annotation must be set after a successful write")
		fmt.Fprintf(GinkgoWriter, "[step 1] full ConfigMap written, hash=%s\n", shortHash(hash5))

		flickerMetalk8sCondition(ctx, metav1.ConditionFalse, "Revalidating", "image re-fetch in progress")
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(result.RequeueAfter).To(BeNumerically(">", 0),
			"reducer must requeue while a component is not ConfigurationRetrieved=True")

		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		appsAfterFlicker := decodeApps(cm.Data["deployed-ui-apps.json"])
		hashAfterFlicker := cm.Annotations[deployedAppsHashAnnotation]
		fmt.Fprintf(GinkgoWriter, "[step 2] flicker observed, ConfigMap unchanged at %d apps, hash=%s\n",
			len(appsAfterFlicker), shortHash(hashAfterFlicker))

		Expect(appsAfterFlicker).To(HaveLen(5),
			"transient flicker must not downgrade the ConfigMap")
		Expect(appNames(appsAfterFlicker)).To(ContainElement("metalk8s-ui"))
		Expect(hashAfterFlicker).To(Equal(hash5),
			"deployed-apps-hash must be unchanged during the flicker window")

		// Snapshot what kubelet would materialise via subPath right now.
		fakeKubeletApps := decodeApps(simulateKubeletMount(ctx, cmName)["deployed-ui-apps.json"])
		fmt.Fprintf(GinkgoWriter, "[step 3] kubelet-equivalent snapshot has %d apps\n", len(fakeKubeletApps))
		Expect(fakeKubeletApps).To(HaveLen(5))
		Expect(appNames(fakeKubeletApps)).To(ContainElement("metalk8s-ui"))

		flickerMetalk8sCondition(ctx, metav1.ConditionTrue, "FetchSucceeded", "ok")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		appsFinal := decodeApps(cm.Data["deployed-ui-apps.json"])
		Expect(appsFinal).To(HaveLen(5))
		Expect(cm.Annotations[deployedAppsHashAnnotation]).To(Equal(hash5),
			"hash must be unchanged through the entire flicker scenario")
		fmt.Fprintf(GinkgoWriter, "[step 4] condition recovered, no churn\n")
	})

	It("falls through to a partial write after the cap so a broken component cannot block the UI", func() {
		ctx := context.Background()
		reconciler := NewScalityUIReconcilerForTest(k8sClient, k8sClient.Scheme())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		flickerMetalk8sCondition(ctx, metav1.ConditionFalse, "FetchFailed", "image not found")

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		Expect(decodeApps(cm.Data["deployed-ui-apps.json"])).To(HaveLen(5),
			"during the wait window the ConfigMap must be unchanged")

		// Rewind firstSeen so the cap is already exceeded on the next call.
		reconciler.setNotReadySinceForTest(uiName, "metalk8s-ui-ns", "metalk8s-ui", time.Now().Add(-2*partialWriteCap))

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())
		// Downstream reducers may set their own RequeueAfter so we do not
		// assert it is zero here; the contract is that the ConfigMap got
		// written despite the missing component.
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		apps := decodeApps(cm.Data["deployed-ui-apps.json"])
		Expect(apps).To(HaveLen(4),
			"after the cap the partial write must go through")
		Expect(appNames(apps)).NotTo(ContainElement("metalk8s-ui"))
		fmt.Fprintf(GinkgoWriter, "[cap] partial ConfigMap written after timeout\n")
	})

	It("does not gate the very first reconcile when the ConfigMap does not yet exist", func() {
		ctx := context.Background()
		reconciler := NewScalityUIReconcilerForTest(k8sClient, k8sClient.Scheme())

		// Pre-existing fixture has 5 ready components. Knock one out
		// before the very first reconcile so we can observe whether the
		// gate fires when no ConfigMap exists yet.
		flickerMetalk8sCondition(ctx, metav1.ConditionFalse, "Revalidating", "image re-fetch in progress")

		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Duration(0)),
			"a fresh install must not be held off by the partial-write gate")

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		apps := decodeApps(cm.Data["deployed-ui-apps.json"])
		Expect(apps).To(HaveLen(4),
			"first install must publish whatever components are ready, not block the UI")
		Expect(appNames(apps)).NotTo(ContainElement("metalk8s-ui"))
		fmt.Fprintf(GinkgoWriter, "[fresh-install] partial ConfigMap written immediately, gate did not engage\n")
	})

	It("clears its first-seen tracker when the ScalityUI is deleted", func() {
		ctx := context.Background()
		reconciler := NewScalityUIReconcilerForTest(k8sClient, k8sClient.Scheme())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		flickerMetalk8sCondition(ctx, metav1.ConditionFalse, "Revalidating", "image re-fetch in progress")

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		// Sanity: the tracker now has an entry for metalk8s-ui under uiName.
		_, present := reconciler.notReadySince.Load(notReadySinceKey(uiName, "metalk8s-ui-ns", "metalk8s-ui"))
		Expect(present).To(BeTrue(), "tracker must have recorded metalk8s-ui as not-ready")

		// Delete the ScalityUI CR and reconcile again. The reconciler
		// observes IsNotFound and must drop every entry under that UI.
		Expect(k8sClient.Delete(ctx, &uiv1alpha1.ScalityUI{ObjectMeta: metav1.ObjectMeta{Name: uiName}})).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
		Expect(err).NotTo(HaveOccurred())

		_, present = reconciler.notReadySince.Load(notReadySinceKey(uiName, "metalk8s-ui-ns", "metalk8s-ui"))
		Expect(present).To(BeFalse(), "tracker must be cleared on CR deletion")

		// Re-create so the AfterEach helpers do not trip.
		Expect(k8sClient.Create(ctx, &uiv1alpha1.ScalityUI{
			ObjectMeta: metav1.ObjectMeta{Name: uiName},
			Spec: uiv1alpha1.ScalityUISpec{
				Image:       "shell-ui:test",
				ProductName: "Test",
			},
		})).To(Succeed())
	})
})

// Helpers

func decodeApps(s string) []uiv1alpha1.DeployedUIApp {
	var apps []uiv1alpha1.DeployedUIApp
	_ = json.Unmarshal([]byte(s), &apps)
	return apps
}

// shortHash returns up to the first 16 characters of a hash for logging,
// without panicking on shorter or empty values.
func shortHash(h string) string {
	if len(h) > 16 {
		return h[:16]
	}
	return h
}

func appNames(apps []uiv1alpha1.DeployedUIApp) []string {
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		names = append(names, a.Name)
	}
	return names
}

// simulateKubeletMount snapshots the ConfigMap's data at this exact moment.
// In real K8s, kubelet on a node does this when materialising a subPath
// volume mount. After the snapshot, ConfigMap updates do NOT propagate to
// the materialised file (this is the documented kubelet subPath limitation:
// https://kubernetes.io/docs/concepts/storage/volumes/#using-subpath ).
// The returned map represents the file content the pod will serve forever.
func simulateKubeletMount(ctx context.Context, cmName types.NamespacedName) map[string]string {
	cm := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
	frozen := make(map[string]string, len(cm.Data))
	for k, v := range cm.Data {
		frozen[k] = v
	}
	return frozen
}

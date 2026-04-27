package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fiveExposerSpecs is a representative cross-namespace fixture used to
// exercise the sort and filtering behaviour of FindAllExposersForUI.
var fiveExposerSpecs = []struct {
	ns, name, comp, basePath string
}{
	{"artesca-ui", "identity-ui-exposer", "identity-ui", "/identity"},
	{"artesca-ui", "artesca-base-ui-exposer", "artesca-base-ui", ""},
	{"metalk8s-ui", "metalk8s-ui-exposer", "metalk8s-ui", "/platform"},
	{"xcore", "xcore-ui-exposer", "xcore-ui", "/storage"},
	{"zenko", "zenko-ui-exposer", "zenko-ui", "/data"},
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := uiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return scheme
}

func newFiveExposers() []client.Object {
	objs := make([]client.Object, 0, len(fiveExposerSpecs))
	for _, s := range fiveExposerSpecs {
		objs = append(objs, &uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: s.ns,
			},
			Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
				ScalityUI:          "shell-ui-cp",
				ScalityUIComponent: s.comp,
				AppHistoryBasePath: s.basePath,
			},
		})
	}
	return objs
}

// TestFindAllExposersForUI_DeterministicOrder asserts that the service
// returns exposers in a stable, content-derived order across many calls.
// Without an explicit sort the underlying client-go indexer returns
// items in randomized Go-map order (see TestRawIndexerIsNonDeterministic),
// which would cause any downstream hash of the result to flap and trigger
// spurious Deployment template re-patches on every reconcile.
func TestFindAllExposersForUI_DeterministicOrder(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newFiveExposers()...).Build()
	svc := NewExposerService(c)

	const iterations = 100
	hashes := map[string]int{}
	var firstResult []uiv1alpha1.ScalityUIComponentExposer

	for i := 0; i < iterations; i++ {
		exposers, err := svc.FindAllExposersForUI(context.Background(), "shell-ui-cp")
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if len(exposers) != len(fiveExposerSpecs) {
			t.Fatalf("iter %d: got %d exposers, want %d", i, len(exposers), len(fiveExposerSpecs))
		}

		// Hash the (namespace, name) ordering so we catch any reordering.
		h := sha256.New()
		for _, e := range exposers {
			fmt.Fprintf(h, "%s/%s|", e.Namespace, e.Name)
		}
		hashes[fmt.Sprintf("%x", h.Sum(nil))]++

		if i == 0 {
			firstResult = exposers
		}
	}

	if len(hashes) != 1 {
		t.Errorf("FindAllExposersForUI returned %d distinct orderings across %d iterations; expected 1 (sort regression)", len(hashes), iterations)
		for h, n := range hashes {
			t.Logf("  %4d times: %s", n, h)
		}
	}

	// Verify the actual order is namespace+name ascending, which is what
	// the sort in FindAllExposersForUI promises.
	want := []string{
		"artesca-ui/artesca-base-ui-exposer",
		"artesca-ui/identity-ui-exposer",
		"metalk8s-ui/metalk8s-ui-exposer",
		"xcore/xcore-ui-exposer",
		"zenko/zenko-ui-exposer",
	}
	for i, e := range firstResult {
		got := e.Namespace + "/" + e.Name
		if got != want[i] {
			t.Errorf("position %d: got %s, want %s", i, got, want[i])
		}
	}
}

// TestFindAllExposersForUI_FilterCorrectness verifies that adding a sort
// has not changed the filtering semantics of FindAllExposersForUI: it must
// still return only the exposers that reference the requested UI name.
func TestFindAllExposersForUI_FilterCorrectness(t *testing.T) {
	scheme := newTestScheme(t)

	// Mix of exposers for two different UIs plus an irrelevant one.
	objs := []client.Object{
		&uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{Name: "cp-1", Namespace: "ns1"},
			Spec:       uiv1alpha1.ScalityUIComponentExposerSpec{ScalityUI: "shell-ui-cp", ScalityUIComponent: "comp-cp-1"},
		},
		&uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{Name: "cp-2", Namespace: "ns2"},
			Spec:       uiv1alpha1.ScalityUIComponentExposerSpec{ScalityUI: "shell-ui-cp", ScalityUIComponent: "comp-cp-2"},
		},
		&uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{Name: "wp-1", Namespace: "ns1"},
			Spec:       uiv1alpha1.ScalityUIComponentExposerSpec{ScalityUI: "shell-ui-wp", ScalityUIComponent: "comp-wp-1"},
		},
		&uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns3"},
			Spec:       uiv1alpha1.ScalityUIComponentExposerSpec{ScalityUI: "some-other-ui", ScalityUIComponent: "comp-other"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	svc := NewExposerService(c)

	cp, err := svc.FindAllExposersForUI(context.Background(), "shell-ui-cp")
	if err != nil {
		t.Fatalf("cp: %v", err)
	}
	if len(cp) != 2 {
		t.Fatalf("shell-ui-cp: got %d, want 2", len(cp))
	}
	for _, e := range cp {
		if e.Spec.ScalityUI != "shell-ui-cp" {
			t.Errorf("shell-ui-cp result contains exposer for %s", e.Spec.ScalityUI)
		}
	}

	wp, err := svc.FindAllExposersForUI(context.Background(), "shell-ui-wp")
	if err != nil {
		t.Fatalf("wp: %v", err)
	}
	if len(wp) != 1 || wp[0].Name != "wp-1" {
		t.Fatalf("shell-ui-wp: got %v, want exactly [wp-1]", wp)
	}

	none, err := svc.FindAllExposersForUI(context.Background(), "no-such-ui")
	if err != nil {
		t.Fatalf("no-such-ui: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("no-such-ui: got %d, want 0", len(none))
	}
}

// TestRawIndexerIsNonDeterministic documents why FindAllExposersForUI
// must sort: client-go cache.ThreadSafeStore (the backend used by the
// controller-runtime informer cache) returns items in randomized order
// driven by Go's randomized map iteration. The test does not assert
// failure (the behaviour is upstream and by design); it logs the
// observed ordering distribution so anyone tempted to remove the sort
// in FindAllExposersForUI sees the evidence first.
func TestRawIndexerIsNonDeterministic(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, s := range fiveExposerSpecs {
		obj := &uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.ns},
		}
		if err := indexer.Add(obj); err != nil {
			t.Fatalf("indexer add: %v", err)
		}
	}

	const iterations = 200
	orderings := map[string]int{}

	for i := 0; i < iterations; i++ {
		items := indexer.List()
		key := ""
		for _, it := range items {
			obj := it.(*uiv1alpha1.ScalityUIComponentExposer)
			key += obj.Namespace + "/" + obj.Name + "|"
		}
		orderings[key]++
	}

	t.Logf("raw indexer.List() observed %d distinct orderings across %d iterations", len(orderings), iterations)
	for k, v := range orderings {
		t.Logf("  %4d times: %s", v, k)
	}

	if len(orderings) == 1 {
		t.Log("note: this run happened to be deterministic (statistically possible). The Go runtime DOES randomize map iteration; do not remove the sort in FindAllExposersForUI based on this single run.")
	}

	// Sanity: the JSON encoding of these orderings must produce the same
	// number of distinct hashes.
	hashes := map[string]int{}
	for k := range orderings {
		j, _ := json.Marshal(k)
		h := sha256.Sum256(j)
		hashes[fmt.Sprintf("%x", h[:])] = orderings[k]
	}
	t.Logf("=> %d distinct downstream hashes if no sort is applied", len(hashes))
}

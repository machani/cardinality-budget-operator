/*
Copyright 2026 Machani Vidyasagar.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
	"github.com/machani/cardinality-budget-operator/internal/enforce"
	"github.com/machani/cardinality-budget-operator/internal/testutil"
	"github.com/machani/cardinality-budget-operator/internal/usage"
)

// testClock is a controllable clock shared by the reconciler and the provider, so grace and settle
// periods can be stepped through without waiting.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// teamRule is a relabel rule a team wrote; enforcement must never change or reorder it.
var teamRule = monitoringv1.RelabelConfig{Action: "replace", SourceLabels: []monitoringv1.LabelName{"pod"}, TargetLabel: "instance"}

// harness runs one budget in its own namespace against a fake Prometheus.
type harness struct {
	ctx      context.Context
	ns       string
	clock    *testClock
	fake     *testutil.FakePrometheus
	provider *usage.PrometheusProvider
	recorder *events.FakeRecorder
	r        *CardinalityBudgetReconciler
}

// newHarness starts the clock at now+offset. Monitors get real creation times from the API server, so a
// positive offset makes existing monitors look older than any drop (drift tests) and a negative offset
// makes them look newer (new-monitor tests).
func newHarness(offset time.Duration) *harness {
	ctx := context.Background()
	clock := &testClock{t: time.Now().Add(offset).Truncate(time.Second)}
	fake := testutil.NewFakePrometheus()
	DeferCleanup(fake.Close)
	// Up for two hours, and its config always counts as reloaded unless a test says otherwise.
	fake.SetRuntimeInfo(clock.Now().Add(-2*time.Hour), true, clock.Now().Add(1000*time.Hour))
	provider, err := usage.NewPrometheusProvider(usage.Settings{
		URL: fake.URL(), NamespaceLabel: "namespace", QueryTimeout: 5 * time.Second,
		BackendWarmup: 15 * time.Minute, Now: clock.Now,
	})
	Expect(err).NotTo(HaveOccurred())
	recorder := events.NewFakeRecorder(200)
	return &harness{
		ctx: ctx, ns: newTestNamespace(ctx), clock: clock, fake: fake, provider: provider, recorder: recorder,
		r: &CardinalityBudgetReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient, Provider: provider, Recorder: recorder,
			MeasureInterval: 5 * time.Minute, MaxMeasurementAge: 10 * time.Minute, SettlePeriod: 10 * time.Minute,
			Now: clock.Now,
		},
	}
}

func (h *harness) budget(spec cardinalityv1alpha1.CardinalityBudgetSpec) {
	Expect(k8sClient.Create(h.ctx, budgetIn(h.ns, spec))).To(Succeed())
}

func (h *harness) serviceMonitor(name string, endpoints int, labels map[string]string) {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.ns, Labels: labels},
		Spec:       monitoringv1.ServiceMonitorSpec{Selector: metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}},
	}
	for range endpoints {
		sm.Spec.Endpoints = append(sm.Spec.Endpoints, monitoringv1.Endpoint{
			Port: "http", MetricRelabelConfigs: []monitoringv1.RelabelConfig{teamRule},
		})
	}
	Expect(k8sClient.Create(h.ctx, sm)).To(Succeed())
}

func (h *harness) monitor(name string) enforce.Monitor {
	sm := &monitoringv1.ServiceMonitor{}
	Expect(k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: name}, sm)).To(Succeed())
	return enforce.Monitor{Object: sm}
}

// usage sets the namespace total and the top metrics (name, series pairs, largest first).
func (h *harness) usage(total float64, top ...any) {
	h.fake.SetResult(h.provider.CountQuery(h.ns), testutil.Sample{Value: total})
	samples := make([]testutil.Sample, 0, len(top)/2)
	for i := 0; i+1 < len(top); i += 2 {
		samples = append(samples, testutil.Sample{Labels: map[string]string{"__name__": top[i].(string)}, Value: float64(top[i+1].(int))})
	}
	h.fake.SetResult(h.provider.TopQuery(h.ns), samples...)
}

func (h *harness) metricSeries(metric string, n float64) {
	h.fake.SetResult(h.provider.MetricSeriesQuery(h.ns, metric), testutil.Sample{Value: n})
}

func (h *harness) reconcile() *cardinalityv1alpha1.CardinalityBudget {
	_, err := h.r.Reconcile(h.ctx, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: h.ns, Name: budgetName}})
	Expect(err).NotTo(HaveOccurred())
	got := &cardinalityv1alpha1.CardinalityBudget{}
	Expect(k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: budgetName}, got)).To(Succeed())
	return got
}

func (h *harness) eventReasons() []string {
	var reasons []string
	for {
		select {
		case e := <-h.recorder.Events:
			reasons = append(reasons, strings.Fields(e)[1])
		default:
			return reasons
		}
	}
}

func (h *harness) patchBudget(mutate func(*cardinalityv1alpha1.CardinalityBudget)) {
	b := &cardinalityv1alpha1.CardinalityBudget{}
	Expect(k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: budgetName}, b)).To(Succeed())
	original := b.DeepCopy()
	mutate(b)
	Expect(k8sClient.Patch(h.ctx, b, client.MergeFrom(original))).To(Succeed())
}

func enforcedReason(b *cardinalityv1alpha1.CardinalityBudget) string {
	if c := meta.FindStatusCondition(b.Status.Conditions, cardinalityv1alpha1.ConditionEnforced); c != nil {
		return c.Reason
	}
	return ""
}

func dropNames(b *cardinalityv1alpha1.CardinalityBudget) []string {
	names := make([]string, 0, len(b.Status.EnforcedDrops))
	for _, d := range b.Status.EnforcedDrops {
		names = append(names, d.Metric)
	}
	return names
}

func enforceSpec() cardinalityv1alpha1.CardinalityBudgetSpec {
	return cardinalityv1alpha1.CardinalityBudgetSpec{
		MaxActiveSeries: 1000, Mode: cardinalityv1alpha1.ModeEnforce,
		Enforcement: cardinalityv1alpha1.EnforcementSpec{
			GracePeriod: metav1.Duration{Duration: time.Minute}, MaxMetricsToDrop: 1, MaxTotalDrops: 5,
			ProtectedMetrics: []string{"up"},
		},
	}
}

// dropBig brings a fresh harness to one applied drop of "big" on monitor "api" (2 endpoints).
func (h *harness) dropBig() *cardinalityv1alpha1.CardinalityBudget {
	h.serviceMonitor("api", 2, nil)
	h.budget(enforceSpec())
	h.usage(1200, "up", 900, "ns:requests:rate5m", 800, "big", 700, "other", 400)
	b := h.reconcile()
	Expect(b.Status.EnforcedDrops).To(BeEmpty(), "no drop before the grace period ends")
	h.clock.Advance(2 * time.Minute)
	b = h.reconcile()
	Expect(dropNames(b)).To(Equal([]string{"big"}), "up is protected and ns:requests:rate5m is a recording rule")
	return b
}

var _ = Describe("Enforce mode", func() {
	It("drops the largest allowed metric after the grace period, on every endpoint, keeping team rules", func() {
		h := newHarness(time.Hour)
		b := h.dropBig()

		d := b.Status.EnforcedDrops[0]
		Expect(d.State).To(Equal(cardinalityv1alpha1.DropPending))
		Expect(d.SeriesAtDrop).To(Equal(int64(700)))
		Expect(d.MonitorCount).To(Equal(int32(1)))
		Expect(b.Status.Phase).To(Equal(cardinalityv1alpha1.PhaseEnforcing))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonApplied))
		Expect(b.Status.LastEnforcementTime).NotTo(BeNil())
		Expect(b.Finalizers).To(ContainElement(cardinalityv1alpha1.CleanupFinalizer))

		sm := h.monitor("api").Object.(*monitoringv1.ServiceMonitor)
		for _, ep := range sm.Spec.Endpoints {
			Expect(ep.MetricRelabelConfigs).To(HaveLen(2))
			Expect(ep.MetricRelabelConfigs[0]).To(Equal(teamRule), "team rule unchanged and first")
			Expect(enforce.IsDropRule(ep.MetricRelabelConfigs[1], "big")).To(BeTrue(), "our rule appended at the end")
		}
		Expect(sm.Annotations[cardinalityv1alpha1.DroppedMetricsAnnotation]).To(Equal(`["big"]`))
		Expect(sm.Labels[cardinalityv1alpha1.EnforcedLabel]).To(Equal("true"))
		Expect(h.eventReasons()).To(ContainElement("Dropped"))
	})

	It("waits for the settle period, then checks effectiveness and takes the next step", func() {
		h := newHarness(time.Hour)
		h.dropBig()

		h.clock.Advance(time.Minute)
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big"}), "second step blocked during the settle period")

		h.metricSeries("big", 0)
		h.clock.Advance(10 * time.Minute)
		b := h.reconcile()
		Expect(b.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1alpha1.DropEffective))
		Expect(dropNames(b)).To(Equal([]string{"big", "other"}), "still over budget: next step after settling")
	})

	It("pauses on an ineffective drop and continues after resume", func() {
		h := newHarness(time.Hour)
		h.dropBig()

		h.metricSeries("big", 500) // more than 10% of 700 remain
		h.clock.Advance(11 * time.Minute)
		b := h.reconcile()
		Expect(b.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1alpha1.DropIneffective))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonIneffective))
		Expect(dropNames(b)).To(Equal([]string{"big"}), "no new drop while paused")

		h.patchBudget(func(b *cardinalityv1alpha1.CardinalityBudget) {
			b.Annotations = map[string]string{cardinalityv1alpha1.ResumeAnnotation: "2026-10-12T12:00:00Z"}
		})
		b = h.reconcile()
		Expect(b.Status.LastResumeToken).To(Equal("2026-10-12T12:00:00Z"))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonApplied))
		Expect(dropNames(b)).To(Equal([]string{"big", "other"}))
	})

	It("pauses when Prometheus does not reload its config after a change", func() {
		h := newHarness(time.Hour)
		h.fake.SetRuntimeInfo(h.clock.Now().Add(-2*time.Hour), true, h.clock.Now().Add(-time.Hour))
		h.dropBig()

		h.clock.Advance(6 * time.Minute)
		b := h.reconcile()
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonConfigReloadFailed))
	})

	It("reports LimitReached when maxTotalDrops is used up", func() {
		h := newHarness(time.Hour)
		h.serviceMonitor("api", 1, nil)
		spec := enforceSpec()
		spec.Enforcement.MaxTotalDrops = 1
		h.budget(spec)
		h.usage(1200, "big", 700, "other", 400)
		h.reconcile()
		h.clock.Advance(2 * time.Minute)
		h.reconcile()
		h.metricSeries("big", 0)
		h.clock.Advance(11 * time.Minute)
		b := h.reconcile()
		Expect(dropNames(b)).To(Equal([]string{"big"}))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonLimitReached))
	})

	It("never writes a monitor while the kill switch is on", func() {
		h := newHarness(time.Hour)
		h.r.EnforcementPaused = true
		h.serviceMonitor("api", 1, nil)
		h.budget(enforceSpec())
		h.usage(1200, "big", 700)
		h.reconcile()
		h.clock.Advance(2 * time.Minute)
		b := h.reconcile()
		Expect(b.Status.EnforcedDrops).To(BeEmpty())
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonPaused))
		Expect(h.monitor("api").Object.GetAnnotations()).NotTo(HaveKey(cardinalityv1alpha1.DroppedMetricsAnnotation))
	})

	It("marks a removed rule Reverted, pauses, never re-applies it, and forgets it on resume", func() {
		h := newHarness(time.Hour)
		h.dropBig()

		sm := h.monitor("api").Object.(*monitoringv1.ServiceMonitor)
		sm.Spec.Endpoints[0].MetricRelabelConfigs, _ = enforce.RemoveLastRule(sm.Spec.Endpoints[0].MetricRelabelConfigs, "big")
		Expect(k8sClient.Update(h.ctx, sm)).To(Succeed()) // what a GitOps revert does

		b := h.reconcile()
		Expect(b.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1alpha1.DropReverted))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonReverted))
		Expect(h.eventReasons()).To(ContainElement(cardinalityv1alpha1.ReasonReverted))
		Expect(enforce.HasRule(h.monitor("api").Object.(*monitoringv1.ServiceMonitor).Spec.Endpoints[0].MetricRelabelConfigs, "big")).
			To(BeFalse(), "a reverted drop is never re-applied")

		h.patchBudget(func(b *cardinalityv1alpha1.CardinalityBudget) {
			b.Annotations = map[string]string{cardinalityv1alpha1.ResumeAnnotation: "r1"}
		})
		b = h.reconcile()
		Expect(dropNames(b)).To(BeEmpty(), "Reverted entries are removed on resume, and no new drop in the same run")
		Expect(b.Status.LastEnforcementTime.Time).To(BeTemporally("==", h.clock.Now()), "the cleanup is the step")

		h.clock.Advance(5 * time.Minute)
		Expect(h.reconcile().Status.EnforcedDrops).To(BeEmpty(), "still settling after the cleanup")
		h.clock.Advance(6 * time.Minute)
		Expect(h.reconcile().Status.EnforcedDrops).To(HaveLen(1), "next step once settled")
	})

	It("extends an active drop to a monitor created after it", func() {
		h := newHarness(-time.Hour) // drops look older than monitors created now
		h.dropBig()

		h.serviceMonitor("worker", 1, nil)
		b := h.reconcile()
		Expect(enforce.HasRule(h.monitor("worker").Object.(*monitoringv1.ServiceMonitor).Spec.Endpoints[0].MetricRelabelConfigs, "big")).To(BeTrue())
		Expect(b.Status.EnforcedDrops[0].MonitorCount).To(Equal(int32(2)))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonApplied), "a new monitor is not drift")
	})

	It("removes owned rules from a monitor that is no longer selected", func() {
		h := newHarness(time.Hour)
		h.serviceMonitor("api", 1, map[string]string{"team": "a"})
		spec := enforceSpec()
		spec.Enforcement.MonitorSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}}
		h.budget(spec)
		h.usage(1200, "big", 700)
		h.reconcile()
		h.clock.Advance(2 * time.Minute)
		h.reconcile()
		Expect(h.monitor("api").Object.GetAnnotations()).To(HaveKey(cardinalityv1alpha1.DroppedMetricsAnnotation))

		sm := h.monitor("api").Object.(*monitoringv1.ServiceMonitor)
		sm.Labels["team"] = "b"
		Expect(k8sClient.Update(h.ctx, sm)).To(Succeed())
		h.reconcile()
		cleaned := h.monitor("api").Object.(*monitoringv1.ServiceMonitor)
		Expect(cleaned.Spec.Endpoints[0].MetricRelabelConfigs).To(Equal([]monitoringv1.RelabelConfig{teamRule}))
		Expect(cleaned.Annotations).NotTo(HaveKey(cardinalityv1alpha1.DroppedMetricsAnnotation))
	})

	It("restores a newly protected metric immediately, even during the settle period", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		h.patchBudget(func(b *cardinalityv1alpha1.CardinalityBudget) {
			b.Spec.Enforcement.ProtectedMetrics = []string{"up", "big"}
		})
		b := h.reconcile()
		Expect(b.Status.EnforcedDrops).To(BeEmpty())
		Expect(h.monitor("api").Object.(*monitoringv1.ServiceMonitor).Spec.Endpoints[0].MetricRelabelConfigs).
			To(Equal([]monitoringv1.RelabelConfig{teamRule}))
	})

	It("restores newest first, one step per settle period, below the warn threshold", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		h.metricSeries("big", 0)
		h.metricSeries("other", 0)
		h.clock.Advance(11 * time.Minute)
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big", "other"}))

		h.usage(500, "big", 10, "other", 10)
		h.clock.Advance(11 * time.Minute)
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big"}), "the most recent drop goes first")
		h.clock.Advance(time.Minute)
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big"}), "one step per settle period")
		h.clock.Advance(11 * time.Minute)
		b := h.reconcile()
		Expect(b.Status.EnforcedDrops).To(BeEmpty())
		Expect(enforcedReason(b)).To(BeEmpty())
	})

	It("restores gradually after leaving Enforce mode, whatever the usage", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		h.patchBudget(func(b *cardinalityv1alpha1.CardinalityBudget) { b.Spec.Mode = cardinalityv1alpha1.ModeWarn })
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big"}), "waits for the settle period")
		h.clock.Advance(11 * time.Minute)
		Expect(h.reconcile().Status.EnforcedDrops).To(BeEmpty())
	})

	It("removes every owned rule and the finalizer when the budget is deleted", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		b := &cardinalityv1alpha1.CardinalityBudget{}
		Expect(k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: budgetName}, b)).To(Succeed())
		Expect(k8sClient.Delete(h.ctx, b)).To(Succeed())

		_, err := h.r.Reconcile(h.ctx, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: h.ns, Name: budgetName}})
		Expect(err).NotTo(HaveOccurred())
		err = k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: budgetName}, b)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "finalizer removed, budget gone")
		sm := h.monitor("api").Object.(*monitoringv1.ServiceMonitor)
		Expect(sm.Spec.Endpoints[0].MetricRelabelConfigs).To(Equal([]monitoringv1.RelabelConfig{teamRule}))
		Expect(sm.Labels).NotTo(HaveKey(cardinalityv1alpha1.EnforcedLabel))
	})

	It("keeps the finalizer while the kill switch is on", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		b := &cardinalityv1alpha1.CardinalityBudget{}
		Expect(k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: budgetName}, b)).To(Succeed())
		Expect(k8sClient.Delete(h.ctx, b)).To(Succeed())

		h.r.EnforcementPaused = true
		h.reconcile()
		Expect(h.monitor("api").Object.GetAnnotations()).To(HaveKey(cardinalityv1alpha1.DroppedMetricsAnnotation))

		h.r.EnforcementPaused = false // let the namespace clean up
		_, err := h.r.Reconcile(h.ctx, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: h.ns, Name: budgetName}})
		Expect(err).NotTo(HaveOccurred())
	})

	It("refuses a monitor write based on an outdated copy (optimistic locking)", func() {
		h := newHarness(time.Hour)
		h.serviceMonitor("api", 1, nil)
		stale := h.monitor("api")

		team := h.monitor("api").Object.(*monitoringv1.ServiceMonitor)
		team.Spec.Endpoints[0].MetricRelabelConfigs = append(team.Spec.Endpoints[0].MetricRelabelConfigs, teamRule)
		Expect(k8sClient.Update(h.ctx, team)).To(Succeed()) // a team edit after we read the monitor

		updated, err := stale.WithDrop("big")
		Expect(err).NotTo(HaveOccurred())
		err = (&enforce.Writer{Client: k8sClient}).Patch(h.ctx, stale, updated)
		Expect(apierrors.IsConflict(err)).To(BeTrue(), "the write must fail instead of overwriting the team's edit")
		Expect(h.monitor("api").Object.(*monitoringv1.ServiceMonitor).Spec.Endpoints[0].MetricRelabelConfigs).To(HaveLen(2))
	})

	It("restores the excess, newest first, when maxTotalDrops is lowered", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		h.metricSeries("big", 0)
		h.metricSeries("other", 0)
		h.clock.Advance(11 * time.Minute)
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big", "other"}))

		h.patchBudget(func(b *cardinalityv1alpha1.CardinalityBudget) {
			b.Spec.Enforcement.MaxTotalDrops = 1
		})
		h.clock.Advance(11 * time.Minute)
		b := h.reconcile()
		Expect(dropNames(b)).To(Equal([]string{"big"}), "the newest drop is restored first; still over budget")
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonApplied))
	})

	It("adopts a drop whose status write was lost and waits a settle period before the next step", func() {
		h := newHarness(time.Hour)
		h.dropBig()

		// The leader wrote the rule, then died before its status write: status still shows the previous,
		// long-settled step and no drop of "big".
		h.clock.Advance(time.Minute)
		b := &cardinalityv1alpha1.CardinalityBudget{}
		Expect(k8sClient.Get(h.ctx, client.ObjectKey{Namespace: h.ns, Name: budgetName}, b)).To(Succeed())
		original := b.DeepCopy()
		b.Status.EnforcedDrops = nil
		b.Status.LastEnforcementTime = &metav1.Time{Time: h.clock.Now().Add(-time.Hour)}
		Expect(k8sClient.Status().Patch(h.ctx, b, client.MergeFrom(original))).To(Succeed())

		h.metricSeries("big", 700)
		b = h.reconcile()
		Expect(dropNames(b)).To(Equal([]string{"big"}), "adopted from the annotation; no second drop in the same run")
		Expect(b.Status.EnforcedDrops[0].SeriesAtDrop).To(Equal(int64(700)))
		Expect(b.Status.LastEnforcementTime.Time).To(BeTemporally("==", h.clock.Now()), "the settle period restarts")

		h.clock.Advance(5 * time.Minute)
		Expect(dropNames(h.reconcile())).To(Equal([]string{"big"}), "still settling")

		h.metricSeries("big", 0)
		h.clock.Advance(6 * time.Minute)
		b = h.reconcile()
		Expect(b.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1alpha1.DropEffective))
		Expect(dropNames(b)).To(Equal([]string{"big", "other"}), "next step once the adopted drop has settled")
	})

	It("does not treat a deleted monitor as drift", func() {
		h := newHarness(time.Hour)
		h.serviceMonitor("worker", 1, nil)
		h.dropBig() // drops on api and worker

		Expect(k8sClient.Delete(h.ctx, h.monitor("worker").Object)).To(Succeed())
		b := h.reconcile()
		Expect(b.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1alpha1.DropPending))
		Expect(b.Status.EnforcedDrops[0].MonitorCount).To(Equal(int32(1)))
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonApplied))
	})

	It("maps every monitor event to its namespace's budget", func() {
		sm := &monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "payments"}}
		Expect(budgetForMonitor(context.Background(), sm)).To(Equal([]reconcile.Request{
			{NamespacedName: client.ObjectKey{Namespace: "payments", Name: budgetName}}}))
	})

	It("applies the global hourly drop limit across budgets", func() {
		// The clocks run 100 hours ahead, so drops left by other tests (within a few hours of now) are
		// older than an hour and do not count.
		first, second := newHarness(100*time.Hour), newHarness(100*time.Hour)
		first.r.MaxDropsPerHour, second.r.MaxDropsPerHour = 1, 1
		first.dropBig()

		second.serviceMonitor("api", 1, nil)
		second.budget(enforceSpec())
		second.usage(1200, "big", 700)
		second.reconcile()
		second.clock.Advance(2 * time.Minute)
		b := second.reconcile()
		Expect(b.Status.EnforcedDrops).To(BeEmpty(), "the other budget used the only drop of this hour")
		Expect(enforcedReason(b)).To(Equal(cardinalityv1alpha1.ReasonRateLimited))
		Expect(second.eventReasons()).To(ContainElement(cardinalityv1alpha1.ReasonRateLimited))

		second.clock.Advance(61 * time.Minute)
		b = second.reconcile()
		Expect(dropNames(b)).To(Equal([]string{"big"}), "the limit clears as the hour moves on")
	})

	It("adds and removes nothing while the measurement is invalid", func() {
		h := newHarness(time.Hour)
		h.dropBig()
		h.fake.SetReady(false)
		h.usage(100) // would trigger a restore if trusted
		h.clock.Advance(30 * time.Minute)
		b := h.reconcile()
		Expect(b.Status.Phase).To(Equal(cardinalityv1alpha1.PhaseUnknown))
		Expect(dropNames(b)).To(Equal([]string{"big"}))
		Expect(h.monitor("api").Object.GetAnnotations()[cardinalityv1alpha1.DroppedMetricsAnnotation]).To(Equal(`["big"]`))
	})
})

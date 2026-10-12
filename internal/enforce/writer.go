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

package enforce

import (
	"context"
	"errors"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

// ErrKillSwitch is returned for every monitor write while --enforcement-paused is on (ADR 0010).
var ErrKillSwitch = errors.New("monitor writes are blocked by --enforcement-paused")

// Writer is the only code that changes monitors. Every write goes through Patch, so the kill switch
// blocks all of them: new drops, restores, extensions and finalizer cleanup alike.
type Writer struct {
	Client client.Client
	// Paused is the kill switch --enforcement-paused.
	Paused bool
}

// Patch writes updated over original as one read-modify-write with optimistic locking: rules, annotation
// and label change together, and a concurrent edit makes the write fail with a conflict instead of being
// overwritten. No server-side apply (ADR 0007).
func (w *Writer) Patch(ctx context.Context, original, updated Monitor) error {
	if w.Paused {
		return ErrKillSwitch
	}
	return w.Client.Patch(ctx, updated.Object,
		client.MergeFromWithOptions(original.Object, client.MergeFromWithOptimisticLock{}))
}

// List returns the namespace's ServiceMonitors and PodMonitors matching selector.
func List(ctx context.Context, c client.Reader, namespace string, selector labels.Selector) ([]Monitor, error) {
	opts := []client.ListOption{client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}}
	services := &monitoringv1.ServiceMonitorList{}
	if err := c.List(ctx, services, opts...); err != nil {
		return nil, err
	}
	pods := &monitoringv1.PodMonitorList{}
	if err := c.List(ctx, pods, opts...); err != nil {
		return nil, err
	}
	monitors := make([]Monitor, 0, len(services.Items)+len(pods.Items))
	for i := range services.Items {
		monitors = append(monitors, Monitor{Object: &services.Items[i]})
	}
	for i := range pods.Items {
		monitors = append(monitors, Monitor{Object: &pods.Items[i]})
	}
	return monitors, nil
}

// ListOwned returns the namespace's monitors carrying the enforced label, selected or not.
func ListOwned(ctx context.Context, c client.Reader, namespace string) ([]Monitor, error) {
	return List(ctx, c, namespace, labels.SelectorFromSet(labels.Set{cardinalityv1alpha1.EnforcedLabel: "true"}))
}

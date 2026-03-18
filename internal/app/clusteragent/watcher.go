package clusteragent

import (
	"context"
	"log/slog"

	"github.com/0x0BSoD/mcp-k8s/pkg/enrichment"
	"github.com/0x0BSoD/mcp-k8s/pkg/events"
	"github.com/0x0BSoD/mcp-k8s/pkg/store"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

const pipelineBuffer = 512

// watcher subscribes to Kubernetes Events via a SharedInformer, normalises
// them and feeds a background pipeline that enriches and stores each event.
type watcher struct {
	clusterName string
	enricher    *enrichment.Enricher
	store       *store.Store
	pipeline    chan events.NormalizedEvent
	factory     informers.SharedInformerFactory
}

func newWatcher(
	clusterName string,
	e *enrichment.Enricher,
	s *store.Store,
	factory informers.SharedInformerFactory,
) *watcher {
	return &watcher{
		clusterName: clusterName,
		enricher:    e,
		store:       s,
		pipeline:    make(chan events.NormalizedEvent, pipelineBuffer),
		factory:     factory,
	}
}

// Start registers the event handler and launches the enrichment pipeline.
// It blocks until the informer cache is synced, then returns.
// The pipeline goroutine runs until ctx is cancelled.
func (w *watcher) Start(ctx context.Context) error {
	informer := w.factory.Core().V1().Events().Informer()

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			w.handle(obj)
		},
		UpdateFunc: func(_, newObj any) {
			w.handle(newObj)
		},
		// DeleteFunc intentionally omitted: deleted events remain in the
		// store until evicted by the ring — useful for post-mortem queries.
	})
	if err != nil {
		return err
	}

	w.factory.Start(ctx.Done())

	slog.Info("waiting for event cache sync")
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return ctx.Err()
	}
	slog.Info("event cache synced")

	go w.runPipeline(ctx)
	return nil
}

// handle normalises a raw k8s Event and pushes it to the pipeline channel.
// Drops the event (with a warning) if the pipeline is full to keep the
// informer callback non-blocking.
func (w *watcher) handle(obj any) {
	raw, ok := obj.(*corev1.Event)
	if !ok {
		return
	}
	ne := events.Normalize(w.clusterName, raw)
	select {
	case w.pipeline <- ne:
	default:
		slog.Warn("pipeline full, dropping event",
			"namespace", ne.Namespace,
			"reason", ne.Reason,
			"object", ne.InvolvedObjectName,
		)
	}
}

// runPipeline reads from the pipeline channel, enriches each event with the
// owner chain, and writes it to the store.
func (w *watcher) runPipeline(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ne := <-w.pipeline:
			chain, err := w.enricher.ResolveOwnerChain(
				ctx,
				ne.Namespace,
				ne.InvolvedObjectKind,
				ne.InvolvedObjectName,
			)
			if err != nil {
				slog.Debug("owner chain resolution failed",
					"namespace", ne.Namespace,
					"kind", ne.InvolvedObjectKind,
					"name", ne.InvolvedObjectName,
					"err", err,
				)
			} else {
				ne.OwnerChain = chain
			}
			w.store.Add(ne)
		}
	}
}

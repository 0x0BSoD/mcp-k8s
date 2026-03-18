package enrichment

import (
	"k8s.io/client-go/kubernetes"
)

// Enricher resolves owner chains and builds object snapshots using the k8s API.
type Enricher struct {
	client kubernetes.Interface
}

func New(client kubernetes.Interface) *Enricher {
	return &Enricher{client: client}
}

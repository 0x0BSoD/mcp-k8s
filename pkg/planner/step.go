package planner

import "time"

// Method names mirror the ClusterAgentService gRPC methods.
type Method string

const (
	MethodListEvents          Method = "ListEvents"
	MethodGetEventsForObject  Method = "GetEventsForObject"
	MethodGetNamespaceSummary Method = "GetNamespaceSummary"
	MethodResolveOwnerChain   Method = "ResolveOwnerChain"
	MethodGetObjectSnapshot   Method = "GetObjectSnapshot"
	MethodGetContainerLogs    Method = "GetContainerLogs"
	MethodGetPodMetrics       Method = "GetPodMetrics"
)

// Step is a single call to be made against the cluster agent.
// Fields are the union of all possible gRPC request parameters;
// only the fields relevant to Method are populated.
type Step struct {
	Method    Method
	Namespace string
	Kind      string    // GetEventsForObject, ResolveOwnerChain, GetObjectSnapshot
	Name      string    // GetEventsForObject, ResolveOwnerChain, GetObjectSnapshot; pod name for GetContainerLogs
	Container string    // GetContainerLogs
	TailLines int32     // GetContainerLogs; 0 = default
	Reason    string    // ListEvents filter
	Since     time.Time // ListEvents, GetNamespaceSummary
	Limit     int       // ListEvents
}

// Plan is an ordered sequence of Steps to execute against one cluster agent.
type Plan struct {
	ClusterName string
	Steps       []Step
}

# mcp-k8s

Example for start:
```shell
  # Register a cluster
  curl -X POST :8080/registry \
    -d '{"cluster_name":"prod-eu","grpc_endpoint":"mcp-cluster-agent.mcp-system:50051"}'

  # Query
  curl -X POST :8080/query \
    -d '{"cluster_name":"prod-eu","namespace":"payments","intent":"ExplainRestarts","since_minutes":30,"mode":"incident-summary"}'

```
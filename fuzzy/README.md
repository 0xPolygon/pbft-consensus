
# Fuzzy IBFT

# Jaeger

Fuzzy uses OpenTracing to profile the execution of the IBFT state machine among the nodes in the cluster. It uses Jaeger to collect the tracing metrics, setup the Jaeger collector with:

```
$ docker run --net=host jaegertracing/all-in-one:1.27
```

## Tests

### TestIBFT_NoIssue

Simple cluster with 5 machines.


# E2E IBFT

# Jaeger

E2E tests uses OpenTracing to profile the execution of the IBFT state machine among the nodes in the cluster. It uses Jaeger to collect the tracing metrics, setup the Jaeger collector with:

```
$ docker run --net=host jaegertracing/all-in-one:1.27
```

You also need to run the OpenCollector to move data to Jaeger from the OpenTelemetry client in IBFT.

```
$ docker run --net=host -v "${PWD}/otel-jaeger-config.yaml":/otel-local-config.yaml otel/opentelemetry-collector --config otel-local-config.yaml
```

## Tests

### TestE2E_NoIssue

Simple cluster with 5 machines.

### TestE2E_LeaderDrop

### TestE2E_Partition_OneMajority

### TestE2E_Partition_NoMajority

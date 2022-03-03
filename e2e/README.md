
# E2E PBFT

# Jaeger

E2E tests uses OpenTracing to profile the execution of the PBFT state machine among the nodes in the cluster. It uses Jaeger to collect the tracing metrics, setup the Jaeger collector with:

```
$ docker run --net=host jaegertracing/all-in-one:1.27
```

You also need to run the OpenCollector to move data to Jaeger from the OpenTelemetry client in PBFT.

```
$ docker run --net=host -v "${PWD}/otel-jaeger-config.yaml":/otel-local-config.yaml otel/opentelemetry-collector --config otel-local-config.yaml
```

## Tests

To log output of nodes into files, set environment variable E2E_LOG_TO_FILES to true.

### TestE2E_NoIssue

Simple cluster with 5 machines.

### TestE2E_NodeDrop

Cluster starts and then one node fails.

### TestE2E_Partition_OneMajority

Cluster of 5 is partitioned in two sets, one with the majority (3) and one without (2).

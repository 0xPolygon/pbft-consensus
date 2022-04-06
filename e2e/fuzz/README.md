- [FUZZ FRAMEWORK](#fuzz-framework)
  - [Daemon](#daemon)
  - [Replay Messages](#replay-messages)
  - [Known issues](#known-issues)

# FUZZ FRAMEWORK

Fuzz framework is an automated software testing framework that involves providing invalid, unexpected, or random data as inputs to a computer program. The program is then monitored for exceptions such as crashes, failing built-in code assertions, or potential memory leaks.

Fuzz framework enables building randomized test sets upon PBFT consensus algorithm. The end goal of the fuzz framework is to ensure that even under stress conditions, we achieve linearity on the PBFT consensus protocol, this is, under some time bounds and conditions (i.e. we cannot produce a block, if there are zero nodes active), a new block is being produced.

## Daemon
Fuzz daemon represents a test runner for fuzz/randomized tests. It is a separate go module/process, which runs alongside the cluster of Pbft nodes.
It runs a Go routine and a single cluster with predefined number of nodes and randomly picks some subset of predefined actions and applies those to the cluster.

Supported actions that can be simulated in fuzz daemon are:
1. Drop Node - simulates dropping of a node in cluster where a single node goes offline, and is not communicating with the rest of the network.
2. Partitions - simulates grouping of nodes in seperate partitions, where each partition only communicates with its member nodes.
3. Flow Map - simulates a message routing mechanism where some of the nodes within the cluster are fully connected, whereas some of them are partially connected (namely only to the subset of peer nodes).

To run fuzz daemon (runner) run the following command:

`go run ./e2e/fuzz/cmd/main.go fuzz-run -nodes={numberOfNodes} -duration={duration}`

e.g., `go run ./e2e/fuzz/cmd/main.go fuzz-run -nodes=5 -duration=1h`

To log node execution to separate log files for easier problem analysis, set environment variable `E2E_LOG_TO_FILES` to `true` before running the `fuzz-run` command. Logs are stored in the parent folder from which `fuzz-run` command was called, inside the `logs-{timestamp}` subfolder. Each node will have its own .log file where name of the file will be the name of the corresponding node.

By default when running `fuzz-run` command, two `.flow` files will be saved containing all the messages that were gossiped during the fuzz daemon execution, alongside some meta data about actions that were simulated during fuzz running. Files are saved inside the parent folder from which `fuzz-run` command was called, inside the `SavedState` subfolder. File containing gossiped messages is named `messages-{timestamp}.flow`, and file containing node and actions meta data is called `metaData-{timestamp}.flow`. These files are needed to replay the fuzz exectuion using the **Replay Messages** feature.

## Replay Messages
Fuzz framework relies on a randomized set of predefined actions, which are applied and reverted on **PolyBFT** nodes cluster. Since its execution is non-deterministic and algorithm failure can be discovered at any time, it is of utmost importance to have trace of triggers which led to the failure state and possibility to replay those triggers on-demand arbitrary number of times. This feature enables in-depth analysis of failure.

Replay takes the provided `messages.flow` and `metaData.flow` files, creates a cluster with appropriate amount of nodes with same name as in fuzz run, pushes all the messages in appropriate node queues and starts the cluster. This enables quick replay of previously run execution and a way to analyze the problem that occurred on demand.

To run replay messages, run the following command:

`go run ./e2e/fuzz/cmd/main.go replay-messages -filesDirectory={directoryWhereFlowFilesAreStored}`

e.g., `go run ./e2e/fuzz/cmd/main.go replay-messages -filesDirectory=../SavedData`

**NOTE**: Replay does not save .flow files on its execution.

## Known issues
When saving messages that are gossiped during the execution of fuzz daemon, messages will be sorted by sequence but they will not be in the order that they were gossiped, since `Gossip` method sends messages to each node asynchronously in separate go routins for each receiver. This means that a `PrePrepare` message may not be first in the `.flow` file since all the other messages are also sent in seperate routines. This does not have an effect or causes an issue on replay, since messages are seperated in different queues in **PolyBFT** state machine depending on its message type (`PrePrepare`, `Prepare`, `Commit`, `RoundChange`).
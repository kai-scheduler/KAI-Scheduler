# Time-Based Fairshare Simulator

A standalone tool for running time-based fairshare simulations for the KAI Scheduler. This tool simulates queue allocations over time and generates allocation history data that can be analyzed to understand fairness behavior.

## Further Reading

For more information about time-based fairshare and the underlying concepts:
- [Time-Based Fairshare Design Document](../../docs/developer/designs/time-based-fairshare/time-based-fairshare.md) - Detailed design and mathematical formulation
- [Fairness Documentation](../../docs/fairness/README.md) - Overview of KAI's fair-share scheduling concepts

## Quick Start

```bash
# Build the simulator
cd /path/to/KAI-Scheduler
go build -o bin/time-based-fairshare-simulator ./cmd/time-based-fairshare-simulator

# Run with example configuration
./bin/time-based-fairshare-simulator -input cmd/time-based-fairshare-simulator/examples/example_config.yaml

# Output will be written to simulation_results.csv
# You can then analyze the results with Python, Excel, or other tools
```

## Overview

The Time-Based Fairshare Simulator creates a Kubernetes test environment using `envtest`, sets up the KAI scheduler components, and simulates workload submissions to multiple queues. It tracks how resources are allocated over time and produces CSV output for analysis.

## Prerequisites

- Go 1.21 or later
- `envtest` binaries (automatically downloaded by the setup-envtest tool if not present)
- **The binary must be executed from within the KAI Scheduler repository** - it needs access to the CRD files in `deployments/kai-scheduler/crds/`

## Building

From the project root directory:

```bash
# Option 1: Use make (recommended - builds for all platforms)
make time-based-fairshare-simulator

# Option 2: Build directly with go
go build -o bin/time-based-fairshare-simulator ./cmd/time-based-fairshare-simulator
```

The Makefile will build:
- `bin/time-based-fairshare-simulator-amd64` - Linux AMD64 binary
- `bin/time-based-fairshare-simulator-arm64` - Linux ARM64 binary  
- Docker image: `registry/local/kai-scheduler/time-aware-simulator:0.0.0`

For local development on macOS/Windows, use the direct `go build` command which builds for your native platform.

## Usage

### Basic Usage

Run a simulation using a YAML configuration file:

```bash
./bin/time-based-fairshare-simulator -input examples/example_config.yaml
```

This will:
1. Read the configuration from `examples/example_config.yaml`
2. Set up a test Kubernetes environment
3. Run the simulation
4. Write results to `simulation_results.csv`

See `examples/example_config.yaml` for a well-commented example configuration.

### Command-Line Options

- `-input <file>`: Path to the YAML configuration file (default: `examples/example_config.yaml`)
  - Use `-input -` to read from stdin
- `-output <file>`: Path to the output CSV file (default: `simulation_results.csv`)
  - Use `-output -` to write to stdout
- `-enable-controller-logs <true/false>`: Print the internal controllers' logs to stdout (default: false)

### Examples

**Read from file, write to default output:**
```bash
./bin/time-based-fairshare-simulator -input examples/simulation_config_oscillating.yaml
```

**Read from stdin, write to stdout:**
```bash
cat examples/example_config.yaml | ./bin/time-based-fairshare-simulator -input - -output -
```

**Verbose output with custom output file:**
```bash
./bin/time-based-fairshare-simulator -input config.yaml -output results.csv
```

**Pipe output directly to analysis tool:**
```bash
./bin/time-based-fairshare-simulator -input config.yaml -output - | python analyze.py
```

## Configuration Format

The input configuration is a YAML file with the following structure:

```yaml
# Number of simulation cycles to run (default: 100)
Cycles: 1024

# Window size for usage tracking in cycles (default: 5)
WindowSize: 256

# Half-life period for exponential decay in cycles (default: 0, disabled)
HalfLifePeriod: 128

# K-value for fairness calculation (default: 1.0)
KValue: 1.0

# Node configuration
Nodes:
  - GPUs: 16      # Number of GPUs per node
    Count: 1      # Number of nodes with this configuration

# Queue definitions
Queues:
  - Name: test-department
    Parent: ""                  # Empty for top-level queue
    Priority: 100               # Queue priority (optional, default: 100)
    DeservedGPUs: 0.0          # Base quota (optional, default: 0)
    Weight: 1.0                # Over-quota weight (optional, default: 1.0)
  
  - Name: test-queue1
    Parent: test-department     # Parent queue
    Priority: 100
    DeservedGPUs: 0.0
    Weight: 1.0
  
  - Name: test-queue2
    Parent: test-department
    Priority: 100
    DeservedGPUs: 0.0
    Weight: 1.0

# Job submissions per queue
Jobs:
  test-queue1:
    GPUs: 16        # GPUs per pod
    NumPods: 1      # Pods per job
    NumJobs: 100    # Number of jobs to submit
  
  test-queue2:
    GPUs: 16
    NumPods: 1
    NumJobs: 100
```

### Configuration Parameters

#### Global Parameters

- `Cycles`: Number of simulation cycles (each cycle is ~10ms). More cycles = longer simulation
- `WindowSize`: Time window (in cycles) for tracking usage history. Affects how quickly the fairness algorithm responds to changes
- `HalfLifePeriod`: Half-life for exponential decay (in seconds). If set to 0, decay is disabled
- `KValue`: Fairness sensitivity parameter. Higher values make the algorithm more aggressive in correcting imbalances

#### Queue Parameters

- **Name**: Unique queue identifier
- **Parent**: Name of parent queue (empty string for department/top-level queues)
- **Priority**: Queue priority (higher = more important)
- **DeservedGPUs**: Base quota allocation (guaranteed resources)
- **Weight**: Over-quota weight (relative share of excess resources)

#### Job Parameters

- **GPUs**: Number of GPUs requested per pod
- **NumPods**: Number of pods in each job
- **NumJobs**: Number of jobs to submit to this queue

## Output Format

The output is a CSV file with the following columns:

- **Time**: Simulation cycle number
- **QueueID**: Name of the queue
- **Allocation**: Actual GPU allocation at this time step
- **FairShare**: Calculated fair share for the queue at this time step

Example output:
```csv
Time,QueueID,Allocation,FairShare
0,test-queue1,16.000000,8.000000
0,test-queue2,0.000000,8.000000
1,test-queue1,16.000000,7.800000
1,test-queue2,0.000000,8.200000
...
```

## Analysis

The CSV output can be analyzed using various tools. For example, python can be used to plot the results.

See [`examples/plot_simple.py`](examples/plot_simple.py) for a complete example that plots both allocation and fair share over time.

### Python/Pandas Example

```python
import pandas as pd
import matplotlib.pyplot as plt

df = pd.read_csv('simulation_results.csv')

# Plot allocations over time
for queue in df['QueueID'].unique():
    queue_data = df[df['QueueID'] == queue]
    plt.plot(queue_data['Time'], queue_data['Allocation'], label=queue)

plt.xlabel('Time')
plt.ylabel('GPU Allocation')
plt.legend()
plt.show()
```

## Example Scenarios

### Two-Queue Oscillation

Demonstrates oscillation behavior between two competing queues:

```yaml
Cycles: 1024
WindowSize: 256
HalfLifePeriod: 128
Nodes:
  - GPUs: 16
    Count: 1
Queues:
  - Name: dept
    Parent: ""
  - Name: queue1
    Parent: dept
  - Name: queue2
    Parent: dept
Jobs:
  queue1:
    GPUs: 16
    NumPods: 1
    NumJobs: 100
  queue2:
    GPUs: 16
    NumPods: 1
    NumJobs: 100
```

## License

Copyright 2025 NVIDIA CORPORATION

SPDX-License-Identifier: Apache-2.0


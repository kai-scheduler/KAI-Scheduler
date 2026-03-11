# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

import argparse
import pandas as pd
import matplotlib.pyplot as plt

# Parse command line arguments
parser = argparse.ArgumentParser(description='Plot simulation results from CSV file')
parser.add_argument('input', nargs='?', default='simulation_results.csv',
                    help='Path to the CSV file (default: simulation_results.csv)')
parser.add_argument('--output', '-o', type=str, default=None,
                    help='Save plot to PNG file instead of displaying it')
args = parser.parse_args()

df = pd.read_csv(args.input)

# Create subplots for allocation and fair share
fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(10, 8), sharex=True)

# Plot allocations over time
for queue in df['QueueID'].unique():
    queue_data = df[df['QueueID'] == queue]
    ax1.plot(queue_data['Time'], queue_data['Allocation'], label=queue)

ax1.set_ylabel('GPU Allocation')
ax1.legend()
ax1.set_title('GPU Allocation Over Time')
ax1.grid(True, alpha=0.3)

# Plot fair share over time
for queue in df['QueueID'].unique():
    queue_data = df[df['QueueID'] == queue]
    ax2.plot(queue_data['Time'], queue_data['FairShare'], label=queue)

ax2.set_xlabel('Time')
ax2.set_ylabel('Fair Share')
ax2.legend()
ax2.set_title('Fair Share Over Time')
ax2.grid(True, alpha=0.3)

plt.tight_layout()

if args.output:
    plt.savefig(args.output, dpi=300, bbox_inches='tight')
    print(f"Plot saved to {args.output}")
else:
    plt.show()


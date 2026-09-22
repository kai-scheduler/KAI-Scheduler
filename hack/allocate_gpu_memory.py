#!/usr/bin/env python3

# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

"""Consume a total GPU-memory budget expressed as a Kubernetes quantity."""

import argparse
import ctypes
import ctypes.util
from decimal import Decimal, InvalidOperation, ROUND_CEILING
import os
import re
import signal
import sys
import threading


QUANTITY_RE = re.compile(
    r"^(?P<number>[+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+))"
    r"(?P<suffix>(?:[eE][+-]?[0-9]+)|[EPTGMK]i|[EPTGMkKmun])?$"
)

DECIMAL_MULTIPLIERS = {
    "": Decimal(1),
    "n": Decimal("1e-9"),
    "u": Decimal("1e-6"),
    "m": Decimal("1e-3"),
    "k": Decimal("1e3"),
    "K": Decimal("1e3"),
    "M": Decimal("1e6"),
    "G": Decimal("1e9"),
    "T": Decimal("1e12"),
    "P": Decimal("1e15"),
    "E": Decimal("1e18"),
}

BINARY_MULTIPLIERS = {
    "%si" % prefix: Decimal(1024) ** exponent
    for exponent, prefix in enumerate("KMGTPE", start=1)
}

MAX_QUANTITY_BYTES = 2**63 - 1


def parse_kubernetes_quantity(value):
    """Return the positive byte count represented by a Kubernetes quantity."""
    match = QUANTITY_RE.fullmatch(value)
    if not match:
        raise ValueError(
            "invalid Kubernetes quantity %r; examples: 8Gi, 12000Mi, 10G" % value
        )

    try:
        number = Decimal(match.group("number"))
    except InvalidOperation as error:
        raise ValueError("invalid numeric value %r" % value) from error

    suffix = match.group("suffix") or ""
    if suffix in BINARY_MULTIPLIERS:
        byte_value = number * BINARY_MULTIPLIERS[suffix]
    elif suffix.startswith(("e", "E")):
        byte_value = number * (Decimal(10) ** int(suffix[1:]))
    else:
        byte_value = number * DECIMAL_MULTIPLIERS[suffix]

    if byte_value <= 0:
        raise ValueError("GPU memory quantity must be greater than zero")

    byte_count = int(byte_value.to_integral_value(rounding=ROUND_CEILING))
    if byte_count > MAX_QUANTITY_BYTES:
        raise ValueError("GPU memory quantity exceeds Kubernetes' maximum value")
    return byte_count


def format_bytes(byte_count):
    gibibytes = Decimal(byte_count) / (Decimal(1024) ** 3)
    return "%d bytes (%.3f GiB)" % (byte_count, gibibytes)


def format_signed_bytes(byte_count):
    sign = "+" if byte_count >= 0 else "-"
    return "%s%s" % (sign, format_bytes(abs(byte_count)))


def load_cuda_runtime():
    candidates = [
        ctypes.util.find_library("cudart"),
        "libcudart.so",
        "libcudart.so.13",
        "libcudart.so.12",
    ]
    errors = []
    for candidate in candidates:
        if not candidate:
            continue
        try:
            return ctypes.CDLL(candidate)
        except OSError as error:
            errors.append("%s: %s" % (candidate, error))
    raise RuntimeError("could not load the CUDA runtime: %s" % "; ".join(errors))


class CudaRuntime:
    def __init__(self):
        self.library = load_cuda_runtime()
        self.library.cudaGetErrorString.argtypes = [ctypes.c_int]
        self.library.cudaGetErrorString.restype = ctypes.c_char_p
        self.library.cudaSetDevice.argtypes = [ctypes.c_int]
        self.library.cudaSetDevice.restype = ctypes.c_int
        self.library.cudaDeviceGetPCIBusId.argtypes = [
            ctypes.c_char_p,
            ctypes.c_int,
            ctypes.c_int,
        ]
        self.library.cudaDeviceGetPCIBusId.restype = ctypes.c_int
        self.library.cudaMemGetInfo.argtypes = [
            ctypes.POINTER(ctypes.c_size_t),
            ctypes.POINTER(ctypes.c_size_t),
        ]
        self.library.cudaMemGetInfo.restype = ctypes.c_int
        self.library.cudaMalloc.argtypes = [ctypes.POINTER(ctypes.c_void_p), ctypes.c_size_t]
        self.library.cudaMalloc.restype = ctypes.c_int
        self.library.cudaMemset.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.c_size_t]
        self.library.cudaMemset.restype = ctypes.c_int
        self.library.cudaDeviceSynchronize.argtypes = []
        self.library.cudaDeviceSynchronize.restype = ctypes.c_int
        self.library.cudaFree.argtypes = [ctypes.c_void_p]
        self.library.cudaFree.restype = ctypes.c_int

    def check(self, result, operation):
        if result == 0:
            return
        message = self.library.cudaGetErrorString(result)
        detail = message.decode("utf-8") if message else "unknown CUDA error"
        raise RuntimeError("%s failed: %s (CUDA error %d)" % (operation, detail, result))

    def set_device(self, device):
        self.check(self.library.cudaSetDevice(device), "cudaSetDevice")

    def pci_bus_id(self, device):
        bus_id = ctypes.create_string_buffer(32)
        self.check(
            self.library.cudaDeviceGetPCIBusId(bus_id, len(bus_id), device),
            "cudaDeviceGetPCIBusId",
        )
        return bus_id.value.decode("ascii")

    def memory_info(self):
        free = ctypes.c_size_t()
        total = ctypes.c_size_t()
        self.check(self.library.cudaMemGetInfo(ctypes.byref(free), ctypes.byref(total)), "cudaMemGetInfo")
        return free.value, total.value

    def allocate(self, byte_count):
        pointer = ctypes.c_void_p()
        self.check(
            self.library.cudaMalloc(ctypes.byref(pointer), ctypes.c_size_t(byte_count)),
            "cudaMalloc",
        )
        return pointer

    def touch(self, pointer, byte_count):
        self.check(
            self.library.cudaMemset(pointer, 0, ctypes.c_size_t(byte_count)),
            "cudaMemset",
        )
        self.check(self.library.cudaDeviceSynchronize(), "cudaDeviceSynchronize")

    def free(self, pointer):
        self.check(self.library.cudaFree(pointer), "cudaFree")


class NvmlProcessInfo(ctypes.Structure):
    _fields_ = [
        ("pid", ctypes.c_uint),
        ("used_gpu_memory", ctypes.c_ulonglong),
        ("gpu_instance_id", ctypes.c_uint),
        ("compute_instance_id", ctypes.c_uint),
    ]


class NvmlMemoryInfo(ctypes.Structure):
    _fields_ = [
        ("total", ctypes.c_ulonglong),
        ("free", ctypes.c_ulonglong),
        ("used", ctypes.c_ulonglong),
    ]


class NvmlRuntime:
    SUCCESS = 0
    ERROR_INSUFFICIENT_SIZE = 7
    VALUE_NOT_AVAILABLE = 2**64 - 1

    def __init__(self):
        candidates = [ctypes.util.find_library("nvidia-ml"), "libnvidia-ml.so.1"]
        errors = []
        for candidate in candidates:
            if not candidate:
                continue
            try:
                self.library = ctypes.CDLL(candidate)
                break
            except OSError as error:
                errors.append("%s: %s" % (candidate, error))
        else:
            raise RuntimeError("could not load NVML: %s" % "; ".join(errors))

        self.library.nvmlInit_v2.argtypes = []
        self.library.nvmlInit_v2.restype = ctypes.c_int
        self.library.nvmlShutdown.argtypes = []
        self.library.nvmlShutdown.restype = ctypes.c_int
        self.library.nvmlErrorString.argtypes = [ctypes.c_int]
        self.library.nvmlErrorString.restype = ctypes.c_char_p
        self.library.nvmlDeviceGetHandleByPciBusId_v2.argtypes = [
            ctypes.c_char_p,
            ctypes.POINTER(ctypes.c_void_p),
        ]
        self.library.nvmlDeviceGetHandleByPciBusId_v2.restype = ctypes.c_int
        self.library.nvmlDeviceGetMemoryInfo.argtypes = [
            ctypes.c_void_p,
            ctypes.POINTER(NvmlMemoryInfo),
        ]
        self.library.nvmlDeviceGetMemoryInfo.restype = ctypes.c_int

        process_function = None
        for name in (
            "nvmlDeviceGetComputeRunningProcesses_v3",
            "nvmlDeviceGetComputeRunningProcesses_v2",
        ):
            process_function = getattr(self.library, name, None)
            if process_function is not None:
                break
        if process_function is None:
            raise RuntimeError("NVML does not expose compute-process accounting")
        process_function.argtypes = [
            ctypes.c_void_p,
            ctypes.POINTER(ctypes.c_uint),
            ctypes.POINTER(NvmlProcessInfo),
        ]
        process_function.restype = ctypes.c_int
        self.get_compute_processes = process_function
        self.check(self.library.nvmlInit_v2(), "nvmlInit_v2")

    def check(self, result, operation):
        if result == self.SUCCESS:
            return
        message = self.library.nvmlErrorString(result)
        detail = message.decode("utf-8") if message else "unknown NVML error"
        raise RuntimeError("%s failed: %s (NVML error %d)" % (operation, detail, result))

    def device_by_pci_bus_id(self, bus_id):
        device = ctypes.c_void_p()
        self.check(
            self.library.nvmlDeviceGetHandleByPciBusId_v2(
                bus_id.encode("ascii"), ctypes.byref(device)
            ),
            "nvmlDeviceGetHandleByPciBusId_v2",
        )
        return device

    def device_memory(self, device):
        memory = NvmlMemoryInfo()
        self.check(
            self.library.nvmlDeviceGetMemoryInfo(device, ctypes.byref(memory)),
            "nvmlDeviceGetMemoryInfo",
        )
        return memory.used

    def process_memory(self, device, pid):
        count = ctypes.c_uint()
        result = self.get_compute_processes(device, ctypes.byref(count), None)
        if result == self.SUCCESS:
            return 0
        if result != self.ERROR_INSUFFICIENT_SIZE:
            self.check(result, "nvmlDeviceGetComputeRunningProcesses")

        for _attempt in range(3):
            capacity = max(count.value, 1)
            processes = (NvmlProcessInfo * capacity)()
            count = ctypes.c_uint(capacity)
            result = self.get_compute_processes(device, ctypes.byref(count), processes)
            if result == self.ERROR_INSUFFICIENT_SIZE:
                continue
            self.check(result, "nvmlDeviceGetComputeRunningProcesses")
            values = [
                process.used_gpu_memory
                for process in processes[: count.value]
                if process.pid == pid
                and process.used_gpu_memory != self.VALUE_NOT_AVAILABLE
            ]
            return sum(values) if values else None
        raise RuntimeError("NVML process list kept changing while it was read")

    def shutdown(self):
        self.check(self.library.nvmlShutdown(), "nvmlShutdown")


def implicit_memory_delta(process_after, device_before, device_after):
    """Return implicit bytes and the source used to attribute them."""
    if process_after is not None and process_after > 0:
        return process_after, "NVML process accounting"
    return max(0, device_after - device_before), "NVML device delta"


def calculate_payload_size(requested_bytes, implicit_bytes):
    """Return the explicit allocation that fits within the total budget."""
    if implicit_bytes >= requested_bytes:
        raise ValueError(
            "implicit/overhead memory uses %s, leaving no room in the %s budget"
            % (format_bytes(implicit_bytes), format_bytes(requested_bytes))
        )
    return requested_bytes - implicit_bytes


def observed_process_memory(process_memory, implicit_bytes, allocation_device_delta):
    """Return observed process memory and the accounting source."""
    if process_memory is not None and process_memory > 0:
        return process_memory, "NVML process accounting"
    return (
        implicit_bytes + max(0, allocation_device_delta),
        "CUDA/NVML device deltas",
    )


def parse_args():
    parser = argparse.ArgumentParser(
        description="Consume and hold a total GPU-memory budget using a Kubernetes quantity."
    )
    parser.add_argument(
        "quantity",
        help="total process GPU-memory budget, for example 8Gi or 12000Mi",
    )
    parser.add_argument("--device", type=int, default=0, help="visible CUDA device index (default: 0)")
    parser.add_argument(
        "--hold-seconds",
        type=float,
        help="release the allocation after this many seconds; hold until interrupted by default",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="validate the quantity and print its byte value without using CUDA",
    )
    return parser.parse_args()


def main():
    args = parse_args()
    nvml = None
    try:
        requested_bytes = parse_kubernetes_quantity(args.quantity)
        if args.hold_seconds is not None and args.hold_seconds < 0:
            raise ValueError("--hold-seconds cannot be negative")

        if args.dry_run:
            print(format_bytes(requested_bytes))
            return 0

        cuda = CudaRuntime()
        pci_bus_id = cuda.pci_bus_id(args.device)
        nvml = NvmlRuntime()
        nvml_device = nvml.device_by_pci_bus_id(pci_bus_id)
        pid = os.getpid()
        process_before = nvml.process_memory(nvml_device, pid)
        device_used_before = nvml.device_memory(nvml_device)

        cuda.set_device(args.device)
        free_after_init, total = cuda.memory_info()
        process_after_init = nvml.process_memory(nvml_device, pid)
        device_used_after_init = nvml.device_memory(nvml_device)
        implicit_bytes, implicit_source = implicit_memory_delta(
            process_after_init,
            device_used_before,
            device_used_after_init,
        )
        payload_bytes = calculate_payload_size(requested_bytes, implicit_bytes)

        print("GPU memory allocation plan", flush=True)
        print("  CUDA device: %d (%s)" % (args.device, pci_bus_id), flush=True)
        print("  GPU total: %s" % format_bytes(total), flush=True)
        print(
            "  NVML whole-device used memory before CUDA initialization: %s"
            % format_bytes(device_used_before),
            flush=True,
        )
        print(
            "  Requested total process budget: %s" % format_bytes(requested_bytes),
            flush=True,
        )
        print(
            "  CUDA initialization/context (implicit, %s): %s"
            % (implicit_source, format_bytes(implicit_bytes)),
            flush=True,
        )
        print(
            "  cudaMalloc payload (explicit): %s" % format_bytes(payload_bytes),
            flush=True,
        )
        print(
            "  Check: implicit + explicit = %s"
            % format_bytes(implicit_bytes + payload_bytes),
            flush=True,
        )
        print(
            "  GPU free after CUDA initialization: %s"
            % format_bytes(free_after_init),
            flush=True,
        )

        pointer = None
        try:
            allocation_baseline_free = free_after_init
            for attempt in range(1, 4):
                pointer = cuda.allocate(payload_bytes)
                cuda.touch(pointer, payload_bytes)
                free_after_allocation, _total = cuda.memory_info()
                process_after_allocation = nvml.process_memory(nvml_device, pid)
                device_used_after_allocation = nvml.device_memory(nvml_device)

                allocation_device_delta = (
                    allocation_baseline_free - free_after_allocation
                )
                observed_total, observed_source = observed_process_memory(
                    process_after_allocation,
                    implicit_bytes,
                    allocation_device_delta,
                )
                other_implicit_bytes = observed_total - implicit_bytes - payload_bytes
                budget_difference = requested_bytes - observed_total

                print("GPU memory allocation attempt %d" % attempt, flush=True)
                print(
                    "  CUDA initialization/context (implicit): %s"
                    % format_bytes(implicit_bytes),
                    flush=True,
                )
                print(
                    "  cudaMalloc payload requested (explicit): %s"
                    % format_bytes(payload_bytes),
                    flush=True,
                )
                print(
                    "  Other allocation/accounting difference (implicit): %s"
                    % format_signed_bytes(other_implicit_bytes),
                    flush=True,
                )
                print(
                    "  Observed total (%s): %s"
                    % (observed_source, format_bytes(observed_total)),
                    flush=True,
                )
                print(
                    "  Requested budget minus observed total: %s"
                    % format_signed_bytes(budget_difference),
                    flush=True,
                )
                if process_after_allocation is None:
                    process_text = (
                        "unavailable (for example, memory may be attributed to MPS)"
                    )
                else:
                    process_text = format_bytes(process_after_allocation)
                print(
                    "  NVML memory attributed to PID %d: %s" % (pid, process_text),
                    flush=True,
                )
                print(
                    "  CUDA free-memory change for this allocation: %s"
                    % format_signed_bytes(allocation_device_delta),
                    flush=True,
                )
                print(
                    "  NVML whole-device used-memory change from startup: %s"
                    % format_signed_bytes(
                        device_used_after_allocation - device_used_before
                    ),
                    flush=True,
                )
                if (
                    process_before is not None
                    and process_after_allocation is not None
                ):
                    process_memory_change = (
                        process_after_allocation - process_before
                    )
                    device_memory_change = (
                        device_used_after_allocation - device_used_before
                    )
                    accounting_discrepancy = (
                        process_memory_change - device_memory_change
                    )
                    if accounting_discrepancy != 0:
                        print(
                            "  Accounting discrepancy "
                            "(PID change minus whole-device change): %s"
                            % format_signed_bytes(accounting_discrepancy),
                            flush=True,
                        )
                        print(
                            "  Note: NVML per-process and whole-device/free-memory "
                            "counters can differ because their accounting scopes and "
                            "granularity differ; budget enforcement uses PID accounting "
                            "when available.",
                            flush=True,
                        )
                print(
                    "  GPU free after allocation: %s"
                    % format_bytes(free_after_allocation),
                    flush=True,
                )

                if budget_difference >= 0:
                    break

                overage = -budget_difference
                cuda.free(pointer)
                pointer = None
                allocation_baseline_free, _total = cuda.memory_info()
                process_after_release = nvml.process_memory(nvml_device, pid)
                device_used_after_release = nvml.device_memory(nvml_device)
                implicit_bytes, implicit_source = implicit_memory_delta(
                    process_after_release,
                    device_used_before,
                    device_used_after_release,
                )
                if attempt == 3:
                    continue
                payload_bytes = calculate_payload_size(
                    requested_bytes,
                    implicit_bytes + overage,
                )
                print(
                    "  Allocation exceeded the budget by %s; retrying with %s"
                    % (format_bytes(overage), format_bytes(payload_bytes)),
                    flush=True,
                )
                print(
                    "  Retained implicit memory before retry (%s): %s"
                    % (implicit_source, format_bytes(implicit_bytes)),
                    flush=True,
                )
            else:
                raise RuntimeError(
                    "could not keep observed GPU memory within the requested budget "
                    "after 3 allocation attempts"
                )

            print(
                "Final allocation is within the requested total GPU-memory budget",
                flush=True,
            )
            print(
                "Device-wide deltas can include concurrent GPU activity; "
                "PID accounting is preferred when NVML exposes it.",
                flush=True,
            )
            print("Allocation active; press Ctrl-C to release it", flush=True)

            stop = threading.Event()

            def request_stop(_signum, _frame):
                stop.set()

            signal.signal(signal.SIGINT, request_stop)
            signal.signal(signal.SIGTERM, request_stop)
            stop.wait(args.hold_seconds)
        finally:
            if pointer is not None:
                cuda.free(pointer)
            free_after_release, _total = cuda.memory_info()
            process_after_release = nvml.process_memory(nvml_device, pid)
            device_used_after_release = nvml.device_memory(nvml_device)
            retained_process = (
                "unavailable"
                if process_after_release is None
                else format_bytes(process_after_release)
            )
            print("GPU memory after cudaFree", flush=True)
            print("  GPU free: %s" % format_bytes(free_after_release), flush=True)
            print(
                "  NVML memory still attributed to PID %d: %s"
                % (pid, retained_process),
                flush=True,
            )
            print(
                "  NVML whole-device used-memory change from startup: %s"
                % format_signed_bytes(device_used_after_release - device_used_before),
                flush=True,
            )
            print("Allocation released; CUDA context remains active until exit", flush=True)
        return 0
    except (KeyError, RuntimeError, ValueError) as error:
        print("error: %s" % error, file=sys.stderr)
        return 1
    finally:
        if nvml is not None:
            try:
                nvml.shutdown()
            except RuntimeError as error:
                print("warning: %s" % error, file=sys.stderr)


if __name__ == "__main__":
    sys.exit(main())

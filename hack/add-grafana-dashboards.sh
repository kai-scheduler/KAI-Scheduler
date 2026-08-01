#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
	echo "Run this script directly instead of sourcing it: hack/add-grafana-dashboards.sh" >&2
	return 1
fi

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

NAMESPACE="${NAMESPACE:-monitoring}"
LOCAL_PORT="${LOCAL_PORT:-8383}"
GRAFANA_SERVICE="${GRAFANA_SERVICE:-}"
GRAFANA_SECRET="${GRAFANA_SECRET:-}"
GRAFANA_SERVICE_PORT="${GRAFANA_SERVICE_PORT:-80}"
DASHBOARD_DIR="${DASHBOARD_DIR:-${REPO_ROOT}/docs/metrics/grafana}"
KEEP_PORT_FORWARD="${KEEP_PORT_FORWARD:-true}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-}"

PF_PID=""
PF_LOG=""
SCRIPT_STATUS=0
ERROR_REPORTED=0

usage() {
	cat <<EOF
Usage: $0 [flags]

Imports the repository Grafana dashboards into a Kubernetes Grafana instance.

Flags:
  -n, --namespace NAME          Grafana namespace (default: ${NAMESPACE})
  -p, --port PORT               Local port-forward port (default: ${LOCAL_PORT})
      --service NAME            Grafana service name (auto-detected by default)
      --service-port PORT       Grafana service port (default: ${GRAFANA_SERVICE_PORT})
      --secret NAME             Grafana admin secret name (auto-detected by default)
      --dashboard-dir PATH      Directory containing dashboard JSON files
      --context NAME            kubectl context to use
      --no-wait                 Stop the port-forward after importing dashboards
  -h, --help                    Show this help

Environment variables with the same names are also supported:
  NAMESPACE, LOCAL_PORT, GRAFANA_SERVICE, GRAFANA_SECRET,
  GRAFANA_SERVICE_PORT, DASHBOARD_DIR, KEEP_PORT_FORWARD, KUBECTL_CONTEXT
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		-n|--namespace)
			NAMESPACE="$2"
			shift 2
			;;
		-p|--port)
			LOCAL_PORT="$2"
			shift 2
			;;
		--service)
			GRAFANA_SERVICE="$2"
			shift 2
			;;
		--service-port)
			GRAFANA_SERVICE_PORT="$2"
			shift 2
			;;
		--secret)
			GRAFANA_SECRET="$2"
			shift 2
			;;
		--dashboard-dir)
			DASHBOARD_DIR="$2"
			shift 2
			;;
		--context)
			KUBECTL_CONTEXT="$2"
			shift 2
			;;
		--no-wait)
			KEEP_PORT_FORWARD="false"
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "Unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

require_command() {
	local command_name="$1"
	if ! command -v "${command_name}" >/dev/null 2>&1; then
		echo "Missing required command: ${command_name}" >&2
		exit 1
	fi
}

kubectl_cmd() {
	local args=()
	if [[ -n "${KUBECTL_CONTEXT}" ]]; then
		args+=(--context "${KUBECTL_CONTEXT}")
	fi
	kubectl "${args[@]}" "$@"
}

resource_exists() {
	local kind="$1"
	local name="$2"
	kubectl_cmd -n "${NAMESPACE}" get "${kind}" "${name}" >/dev/null 2>&1
}

discover_grafana_secret() {
	if [[ -n "${GRAFANA_SECRET}" ]]; then
		if ! resource_exists secret "${GRAFANA_SECRET}"; then
			echo "Grafana secret ${GRAFANA_SECRET} was not found in namespace ${NAMESPACE}" >&2
			exit 1
		fi
		return
	fi

	local candidate
	candidate="$(kubectl_cmd -n "${NAMESPACE}" get secrets -o json | jq -r '
		first(.items[]
			| select(.data["admin-user"] and .data["admin-password"])
			| .metadata.name) // empty
	')"

	if [[ -z "${candidate}" ]]; then
		echo "Could not find a Grafana admin secret in namespace ${NAMESPACE}" >&2
		echo "Pass one explicitly with --secret or GRAFANA_SECRET." >&2
		exit 1
	fi

	GRAFANA_SECRET="${candidate}"
}

discover_grafana_service() {
	if [[ -n "${GRAFANA_SERVICE}" ]]; then
		if ! resource_exists service "${GRAFANA_SERVICE}"; then
			echo "Grafana service ${GRAFANA_SERVICE} was not found in namespace ${NAMESPACE}" >&2
			exit 1
		fi
		return
	fi

	if resource_exists service "${GRAFANA_SECRET}"; then
		GRAFANA_SERVICE="${GRAFANA_SECRET}"
		return
	fi

	local candidate
	candidate="$(kubectl_cmd -n "${NAMESPACE}" get services -o json | jq -r '
		first(.items[]
			| select(.metadata.labels["app.kubernetes.io/name"] == "grafana")
			| .metadata.name) // empty
	')"

	if [[ -z "${candidate}" ]]; then
		echo "Could not find a Grafana service in namespace ${NAMESPACE}" >&2
		echo "Pass one explicitly with --service or GRAFANA_SERVICE." >&2
		exit 1
	fi

	GRAFANA_SERVICE="${candidate}"
}

decode_secret_key() {
	local key="$1"
	kubectl_cmd -n "${NAMESPACE}" get secret "${GRAFANA_SECRET}" -o json \
		| jq -r --arg key "${key}" '.data[$key] // empty' \
		| base64 --decode
}

cleanup() {
	if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" >/dev/null 2>&1; then
		kill "${PF_PID}" >/dev/null 2>&1 || true
		wait "${PF_PID}" >/dev/null 2>&1 || true
	fi
	if [[ "${SCRIPT_STATUS}" -eq 0 && -n "${PF_LOG}" && -f "${PF_LOG}" ]]; then
		rm -f "${PF_LOG}"
	fi
}

on_error() {
	local exit_code="$?"
	local line_number="$1"

	SCRIPT_STATUS="${exit_code}"
	ERROR_REPORTED=1
	echo "Failed on line ${line_number} with exit code ${exit_code}." >&2
	if [[ -n "${PF_LOG}" && -f "${PF_LOG}" ]]; then
		echo "Port-forward log preserved at: ${PF_LOG}" >&2
		echo "Last port-forward log lines:" >&2
		tail -50 "${PF_LOG}" >&2 || true
	fi
	exit "${exit_code}"
}

on_exit() {
	local exit_code="$?"

	SCRIPT_STATUS="${exit_code}"
	if [[ "${exit_code}" -ne 0 && "${ERROR_REPORTED}" -eq 0 ]]; then
		echo "Script exited with status ${exit_code}." >&2
		if [[ -n "${PF_LOG}" && -f "${PF_LOG}" ]]; then
			echo "Port-forward log preserved at: ${PF_LOG}" >&2
		fi
	fi
	cleanup
}

on_interrupt() {
	SCRIPT_STATUS=130
	echo "Interrupted. Stopping port-forward." >&2
	cleanup
	exit 130
}

on_terminate() {
	SCRIPT_STATUS=143
	echo "Terminated. Stopping port-forward." >&2
	cleanup
	exit 143
}

trap 'on_error ${LINENO}' ERR
trap on_exit EXIT
trap on_interrupt INT
trap on_terminate TERM

wait_for_grafana() {
	local url="$1"

	for _ in $(seq 1 30); do
		if curl -fsS "${url}/api/health" >/dev/null 2>&1; then
			return 0
		fi
		if [[ -n "${PF_PID}" ]] && ! kill -0 "${PF_PID}" >/dev/null 2>&1; then
			echo "Port-forward exited before Grafana became reachable." >&2
			if [[ -f "${PF_LOG}" ]]; then
				cat "${PF_LOG}" >&2
			fi
			exit 1
		fi
		sleep 1
	done

	echo "Timed out waiting for Grafana at ${url}" >&2
	if [[ -f "${PF_LOG}" ]]; then
		cat "${PF_LOG}" >&2
	fi
	exit 1
}

start_port_forward() {
	local url="$1"

	if curl -fsS "${url}/api/health" >/dev/null 2>&1; then
		echo "Using existing Grafana endpoint at ${url}"
		return
	fi

	PF_LOG="$(mktemp)"
	echo "Starting port-forward: ${url} -> svc/${GRAFANA_SERVICE}:${GRAFANA_SERVICE_PORT}"
	kubectl_cmd -n "${NAMESPACE}" port-forward "svc/${GRAFANA_SERVICE}" "${LOCAL_PORT}:${GRAFANA_SERVICE_PORT}" >"${PF_LOG}" 2>&1 &
	PF_PID="$!"

	wait_for_grafana "${url}"
}

import_dashboard() {
	local dashboard_file="$1"
	local url="$2"
	local username="$3"
	local password="$4"
	local response_file
	local status_code
	local title

	response_file="$(mktemp)"
	title="$(jq -r '.title // input_filename' "${dashboard_file}")"

	status_code="$(
		jq -c '{dashboard: (. | .id = null), overwrite: true, folderId: 0}' "${dashboard_file}" \
			| curl -sS \
				-u "${username}:${password}" \
				-H "Content-Type: application/json" \
				-X POST \
				-o "${response_file}" \
				-w "%{http_code}" \
				--data-binary @- \
				"${url}/api/dashboards/db"
	)"

	if [[ "${status_code}" != "200" && "${status_code}" != "202" ]]; then
		echo "Failed to import ${dashboard_file} (${status_code})" >&2
		cat "${response_file}" >&2
		rm -f "${response_file}"
		exit 1
	fi

	local dashboard_url
	dashboard_url="$(jq -r '.url // empty' "${response_file}")"
	rm -f "${response_file}"

	if [[ -n "${dashboard_url}" ]]; then
		echo "Imported: ${title} (${url}${dashboard_url})"
	else
		echo "Imported: ${title}"
	fi
}

require_command kubectl
require_command curl
require_command jq
require_command base64

if [[ ! -d "${DASHBOARD_DIR}" ]]; then
	echo "Dashboard directory does not exist: ${DASHBOARD_DIR}" >&2
	exit 1
fi

shopt -s nullglob
dashboards=("${DASHBOARD_DIR}"/*.json)
if [[ "${#dashboards[@]}" -eq 0 ]]; then
	echo "No dashboard JSON files found in ${DASHBOARD_DIR}" >&2
	exit 1
fi

discover_grafana_secret
discover_grafana_service

GRAFANA_USERNAME="$(decode_secret_key admin-user)"
GRAFANA_PASSWORD="$(decode_secret_key admin-password)"

if [[ -z "${GRAFANA_USERNAME}" || -z "${GRAFANA_PASSWORD}" ]]; then
	echo "Secret ${GRAFANA_SECRET} does not contain admin-user/admin-password" >&2
	exit 1
fi

GRAFANA_URL="http://localhost:${LOCAL_PORT}"

start_port_forward "${GRAFANA_URL}"

echo
echo "Grafana URL: ${GRAFANA_URL}"
echo "Grafana service: ${NAMESPACE}/${GRAFANA_SERVICE}"
echo "Grafana secret: ${NAMESPACE}/${GRAFANA_SECRET}"
echo "Username: ${GRAFANA_USERNAME}"
echo "Password: ${GRAFANA_PASSWORD}"
echo

for dashboard in "${dashboards[@]}"; do
	import_dashboard "${dashboard}" "${GRAFANA_URL}" "${GRAFANA_USERNAME}" "${GRAFANA_PASSWORD}"
done

echo
echo "Imported ${#dashboards[@]} dashboard(s)."

if [[ -n "${PF_PID}" && "${KEEP_PORT_FORWARD}" == "true" ]]; then
	echo "Port-forward is running. Press Ctrl-C to stop it."
	wait "${PF_PID}"
else
	cleanup
fi

#!/usr/bin/env bash

POLL_OUTCOME=""
POLL_ERROR_CLASS=""
POLL_ATTEMPTS=0
POLL_OBSERVATIONS=()
POLL_AGREEMENT=""
POLL_CANCEL_REQUESTED=0
POLL_LAST_ATTEMPT_OUTCOME=""

classify_probe_result() {
  local protocol="$1" status="$2" error_sample="$3"

  if [[ "$protocol" != "TCP" ]]; then
    POLL_ATTEMPT_OUTCOME="unsupported"
    POLL_ATTEMPT_ERROR_CLASS="unsupported_probe_protocol"
  elif [[ "$status" -eq 0 ]]; then
    POLL_ATTEMPT_OUTCOME="success"
    POLL_ATTEMPT_ERROR_CLASS=""
  elif [[ "$status" -eq 124 || "$status" -eq 137 ]] ||
    grep -Eqi 'timed out|i/o timeout|operation timed out' <<< "$error_sample"; then
    POLL_ATTEMPT_OUTCOME="timeout"
    POLL_ATTEMPT_ERROR_CLASS="probe_timeout"
  elif grep -Eqi 'forbidden|unable to upgrade connection|pod .* not found|container .* not found|is not running|not authorized|error from server|error: unable to connect' <<< "$error_sample"; then
    POLL_ATTEMPT_OUTCOME="incomplete_setup"
    if grep -Eqi 'forbidden|not authorized' <<< "$error_sample"; then
      POLL_ATTEMPT_ERROR_CLASS="exec_permission_denied"
    else
      POLL_ATTEMPT_ERROR_CLASS="source_workload_or_exec_unavailable"
    fi
  elif grep -Eqi 'executable file not found|nc: not found|command not found|applet not found|invalid option|usage:' <<< "$error_sample"; then
    POLL_ATTEMPT_OUTCOME="unsupported"
    POLL_ATTEMPT_ERROR_CLASS="unsupported_probe_utility"
  else
    POLL_ATTEMPT_OUTCOME="connection_failure"
    POLL_ATTEMPT_ERROR_CLASS="connection_attempt_failed"
  fi
}

poll_probe() {
  local expected="$1" attempt_function="$2"
  local max_attempts="${PROBE_POLL_MAX_ATTEMPTS:-3}"
  local retry_interval="${PROBE_POLL_INTERVAL_SECONDS:-0.5}"
  local stable_required="${PROBE_POLL_STABLE_COUNT:-2}"
  local expected_observation consecutive=0 last_observation=""

  case "$expected" in
    allowed_by_model) expected_observation="success" ;;
    denied_by_model) expected_observation="connection_failure" ;;
    *)
      POLL_OUTCOME="inconclusive"
      POLL_ERROR_CLASS="unsupported_model_state"
      POLL_AGREEMENT="inconclusive"
      return 0
      ;;
  esac

  POLL_OUTCOME=""
  POLL_ERROR_CLASS=""
  POLL_ATTEMPTS=0
  POLL_OBSERVATIONS=()
  POLL_AGREEMENT=""
  POLL_LAST_ATTEMPT_OUTCOME=""

  for ((attempt = 1; attempt <= max_attempts; attempt++)); do
    if (( POLL_CANCEL_REQUESTED )); then
      POLL_OUTCOME="inconclusive"
      POLL_ERROR_CLASS="cancelled"
      POLL_AGREEMENT="inconclusive"
      return 0
    fi

    POLL_ATTEMPTS="$attempt"
    POLL_ATTEMPT_OUTCOME=""
    POLL_ATTEMPT_ERROR_CLASS=""
    "$attempt_function"
    POLL_LAST_ATTEMPT_OUTCOME="$POLL_ATTEMPT_OUTCOME"
    POLL_OBSERVATIONS+=("$POLL_ATTEMPT_OUTCOME")

    case "$POLL_ATTEMPT_OUTCOME" in
      success|connection_failure)
        if [[ "$POLL_ATTEMPT_OUTCOME" == "$last_observation" ]]; then
          ((consecutive += 1))
        else
          consecutive=1
          last_observation="$POLL_ATTEMPT_OUTCOME"
        fi
        if (( consecutive >= stable_required )); then
          POLL_OUTCOME="$POLL_ATTEMPT_OUTCOME"
          POLL_ERROR_CLASS="$POLL_ATTEMPT_ERROR_CLASS"
          if [[ "$POLL_OUTCOME" == "$expected_observation" ]]; then
            POLL_AGREEMENT="true"
          else
            POLL_AGREEMENT="false"
          fi
          return 0
        fi
        ;;
      timeout|unsupported|incomplete_setup)
        POLL_OUTCOME="$POLL_ATTEMPT_OUTCOME"
        POLL_ERROR_CLASS="$POLL_ATTEMPT_ERROR_CLASS"
        POLL_AGREEMENT="inconclusive"
        return 0
        ;;
      *)
        POLL_OUTCOME="inconclusive"
        POLL_ERROR_CLASS="${POLL_ATTEMPT_ERROR_CLASS:-unclassified_probe_result}"
        POLL_AGREEMENT="inconclusive"
        return 0
        ;;
    esac

    if (( POLL_CANCEL_REQUESTED )); then
      POLL_OUTCOME="inconclusive"
      POLL_ERROR_CLASS="cancelled"
      POLL_AGREEMENT="inconclusive"
      return 0
    fi
    if (( attempt < max_attempts )); then
      sleep "$retry_interval"
    fi
  done

  POLL_OUTCOME="inconclusive"
  POLL_ERROR_CLASS="no_stable_observation"
  POLL_AGREEMENT="inconclusive"
}

record_incomplete_setup() {
  local output="$1" stage="$2" error_class="$3" message="$4"
  python3 - "$output" "$stage" "$error_class" "$message" <<'PY'
import json
import sys
from datetime import datetime, timezone

output, stage, error_class, message = sys.argv[1:]
record = {
    "scenario": stage,
    "stage": "setup",
    "outcome": "incomplete_setup",
    "probeErrorClassification": error_class,
    "modelObservationAgreement": None,
    "policyCausation": "not-established",
    "attemptedAt": datetime.now(timezone.utc).isoformat(),
    "error": message,
    "environment": {
        "cluster": "pci-phase6",
        "context": "kind-pci-phase6",
        "kubernetes": "v1.31.4",
        "cni": "Calico v3.30.0",
    },
    "limitations": ["Setup did not complete; no connectivity observation is available."],
}
with open(output, "a", encoding="utf-8") as stream:
    stream.write(json.dumps(record, sort_keys=True) + "\n")
PY
}

record_probe_result() {
  local output="$1"
  shift
  python3 - "$@" "$output" <<'PY'
import json
import sys

name, before, modeled, change, observed, agrees, error_class, attempts, attempt_outcomes, kubernetes, cni, busybox, nginx, context, source, destination, port, attempted_at, duration_ms, output = sys.argv[1:]
record = {
    "scenario": name,
    "baselineModeled": before,
    "modeled": modeled,
    "modeledChange": change,
    "observed": observed,
    "probeErrorClassification": error_class or None,
    "probeAttempts": int(attempts),
    "probeAttemptOutcomes": attempt_outcomes.split() if attempt_outcomes else [],
    "modelObservationAgreement": True if agrees == "true" else (False if agrees == "false" else None),
    "environment": {
        "kubernetes": kubernetes,
        "cni": "Calico " + cni,
        "cluster": "pci-phase6",
        "context": context,
        "testImages": {"busybox": busybox, "nginx": nginx},
    },
    "flow": {"source": source, "destinationPodIP": destination, "protocol": "TCP", "port": int(port)},
    "attemptedAt": attempted_at,
    "durationMillis": int(duration_ms),
    "policyCausation": "not-established-by-connectivity-probe",
    "limitations": ["Direct Pod-IP TCP probe; no Service/NAT/DNS path was tested.", "Observed outcomes do not prove NetworkPolicy causation."],
    "alternativeExplanations": [
        "CNI or dataplane not enforcing the expected policy",
        "Pod readiness, routing, or node firewall issue",
        "Probe utility or timeout limitation",
    ],
}
with open(output, "a", encoding="utf-8") as stream:
    stream.write(json.dumps(record, sort_keys=True) + "\n")
PY
}

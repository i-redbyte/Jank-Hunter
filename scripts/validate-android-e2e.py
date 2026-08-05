#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from pathlib import Path
from typing import Any


MISSING = object()
RULE_KEYS = {
    "contains",
    "equals",
    "maximum",
    "maximum_length",
    "minimum",
    "minimum_length",
    "not_equals",
    "one_of",
    "type",
}
ASSERTION_KEYS = {"cardinality", "expect", "field", "id", "mode", "path", "where"}
TOP_LEVEL_KEYS = {"assertions", "relations", "schema", "warnings"}
RELATION_KEYS = {"id", "left", "operator", "right"}


class ContractFailure(Exception):
    pass


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--report", required=True)
    parser.add_argument("--contract", required=True)
    parser.add_argument("--diagnostics-supplied", action="store_true")
    return parser.parse_args()


def read_json(path: Path, label: str) -> Any:
    try:
        with path.open(encoding="utf-8") as source:
            return json.load(source)
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ContractFailure(f"cannot read {label} {path}: {error}") from error


def resolve(value: Any, path: str) -> Any:
    current = value
    for part in path.split("."):
        if not part or not isinstance(current, dict) or part not in current:
            return MISSING
        current = current[part]
    return current


def type_matches(value: Any, expected: str) -> bool:
    checks = {
        "array": lambda candidate: isinstance(candidate, list),
        "boolean": lambda candidate: isinstance(candidate, bool),
        "integer": lambda candidate: isinstance(candidate, int) and not isinstance(candidate, bool),
        "null": lambda candidate: candidate is None,
        "number": lambda candidate: isinstance(candidate, (int, float))
        and not isinstance(candidate, bool)
        and math.isfinite(candidate),
        "object": lambda candidate: isinstance(candidate, dict),
        "string": lambda candidate: isinstance(candidate, str),
    }
    if expected not in checks:
        raise ContractFailure(f"unsupported type rule: {expected!r}")
    return checks[expected](value)


def numeric(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def validate_rule(label: str, actual: Any, rule: Any) -> list[str]:
    if not isinstance(rule, dict) or not rule:
        raise ContractFailure(f"{label}: expectation must be a non-empty object")
    unknown = set(rule) - RULE_KEYS
    if unknown:
        raise ContractFailure(f"{label}: unsupported expectation keys: {sorted(unknown)}")
    failures = []
    expected_type = rule.get("type")
    if expected_type is not None:
        if not isinstance(expected_type, str):
            raise ContractFailure(f"{label}: type expectation must be a string")
        if not type_matches(actual, expected_type):
            failures.append(f"{label}: expected type {expected_type}, found {type(actual).__name__}")
            return failures
    if "equals" in rule and actual != rule["equals"]:
        failures.append(f"{label}: expected {rule['equals']!r}, found {actual!r}")
    if "not_equals" in rule and actual == rule["not_equals"]:
        failures.append(f"{label}: value must differ from {rule['not_equals']!r}")
    if "one_of" in rule:
        expected = rule["one_of"]
        if not isinstance(expected, list) or not expected:
            raise ContractFailure(f"{label}: one_of must be a non-empty array")
        if actual not in expected:
            failures.append(f"{label}: expected one of {expected!r}, found {actual!r}")
    for key, relation in (("minimum", "at least"), ("maximum", "at most")):
        if key not in rule:
            continue
        expected = rule[key]
        if not numeric(expected):
            raise ContractFailure(f"{label}: {key} must be a finite number")
        if not numeric(actual):
            failures.append(f"{label}: expected a finite number, found {actual!r}")
        elif key == "minimum" and actual < expected:
            failures.append(f"{label}: expected {relation} {expected}, found {actual}")
        elif key == "maximum" and actual > expected:
            failures.append(f"{label}: expected {relation} {expected}, found {actual}")
    for key, relation in (("minimum_length", "at least"), ("maximum_length", "at most")):
        if key not in rule:
            continue
        expected = rule[key]
        if isinstance(expected, bool) or not isinstance(expected, int) or expected < 0:
            raise ContractFailure(f"{label}: {key} must be a non-negative integer")
        if not isinstance(actual, (str, list, dict)):
            failures.append(f"{label}: length is not defined for {actual!r}")
        elif key == "minimum_length" and len(actual) < expected:
            failures.append(f"{label}: expected length {relation} {expected}, found {len(actual)}")
        elif key == "maximum_length" and len(actual) > expected:
            failures.append(f"{label}: expected length {relation} {expected}, found {len(actual)}")
    if "contains" in rule:
        expected = rule["contains"]
        if not isinstance(actual, (str, list, dict)):
            failures.append(f"{label}: contains is not supported for {actual!r}")
        elif expected not in actual:
            failures.append(f"{label}: expected to contain {expected!r}, found {actual!r}")
    return failures


def matches(row: Any, expected: dict[str, Any]) -> bool:
    return isinstance(row, dict) and all(resolve(row, path) == value for path, value in expected.items())


def validate_assertion(report: dict[str, Any], assertion: Any) -> list[str]:
    if not isinstance(assertion, dict):
        raise ContractFailure("each assertion must be an object")
    unknown = set(assertion) - ASSERTION_KEYS
    if unknown:
        raise ContractFailure(f"assertion has unsupported keys: {sorted(unknown)}")
    identifier = assertion.get("id")
    path = assertion.get("path")
    if not isinstance(identifier, str) or not identifier.strip():
        raise ContractFailure("assertion id must be a non-empty string")
    if not isinstance(path, str) or not path.strip():
        raise ContractFailure(f"{identifier}: assertion path must be a non-empty string")
    resolved = resolve(report, path)
    if resolved is MISSING:
        return [f"{identifier}: report path is missing: {path}"]
    where = assertion.get("where")
    field = assertion.get("field")
    if where is None:
        selected = [resolved]
    else:
        if not isinstance(where, dict) or not where:
            raise ContractFailure(f"{identifier}: where must be a non-empty object")
        if not isinstance(resolved, list):
            return [f"{identifier}: where requires an array at {path}"]
        selected = [row for row in resolved if matches(row, where)]
    if field is not None:
        if not isinstance(field, str) or not field:
            raise ContractFailure(f"{identifier}: field must be a non-empty string")
        selected = [resolve(row, field) for row in selected]
        missing_fields = sum(value is MISSING for value in selected)
        if missing_fields:
            return [f"{identifier}: field {field} is missing in {missing_fields} selected row(s)"]
    failures = []
    cardinality = assertion.get("cardinality")
    if cardinality is not None:
        failures.extend(validate_rule(f"{identifier}.cardinality", len(selected), cardinality))
    expectation = assertion.get("expect")
    if expectation is None:
        if cardinality is None:
            raise ContractFailure(f"{identifier}: assertion requires expect or cardinality")
        return failures
    if not selected:
        if cardinality is None:
            failures.append(f"{identifier}: no values selected for expectation")
        return failures
    mode = assertion.get("mode", "all")
    if mode not in {"all", "any"}:
        raise ContractFailure(f"{identifier}: mode must be all or any")
    results = [validate_rule(f"{identifier}[{index}]", value, expectation) for index, value in enumerate(selected)]
    if mode == "all":
        for result in results:
            failures.extend(result)
    elif all(result for result in results):
        failures.append(f"{identifier}: no selected value satisfied the expectation")
    return failures


def validate_relation(report: dict[str, Any], relation: Any) -> list[str]:
    if not isinstance(relation, dict):
        raise ContractFailure("each relation must be an object")
    unknown = set(relation) - RELATION_KEYS
    if unknown:
        raise ContractFailure(f"relation has unsupported keys: {sorted(unknown)}")
    identifier = relation.get("id")
    left_path = relation.get("left")
    right_path = relation.get("right")
    operator = relation.get("operator", "equals")
    if not all(isinstance(value, str) and value for value in (identifier, left_path, right_path)):
        raise ContractFailure("relation id, left and right must be non-empty strings")
    left = resolve(report, left_path)
    right = resolve(report, right_path)
    if left is MISSING or right is MISSING:
        return [f"{identifier}: relation path is missing: {left_path} or {right_path}"]
    operations = {
        "equals": lambda: left == right,
        "greater_or_equal": lambda: numeric(left) and numeric(right) and left >= right,
        "less_or_equal": lambda: numeric(left) and numeric(right) and left <= right,
        "not_equals": lambda: left != right,
    }
    if operator not in operations:
        raise ContractFailure(f"{identifier}: unsupported relation operator {operator!r}")
    if operations[operator]():
        return []
    return [f"{identifier}: relation failed: {left_path}={left!r} {operator} {right_path}={right!r}"]


def validate_warnings(report: dict[str, Any], policy: Any, diagnostics_supplied: bool) -> list[str]:
    if policy is None:
        return []
    if not isinstance(policy, dict):
        raise ContractFailure("warnings policy must be an object")
    unknown = set(policy) - {"allowed_without_diagnostics", "forbidden_fragments", "path"}
    if unknown:
        raise ContractFailure(f"warnings policy has unsupported keys: {sorted(unknown)}")
    path = policy.get("path", "Warnings")
    warnings = resolve(report, path)
    if warnings is MISSING:
        return [f"warnings path is missing: {path}"]
    if warnings is None:
        warnings = []
    if not isinstance(warnings, list) or any(not isinstance(item, str) for item in warnings):
        return [f"{path} must be null or an array of strings"]
    allowed = policy.get("allowed_without_diagnostics", [])
    forbidden = policy.get("forbidden_fragments", [])
    if not isinstance(allowed, list) or any(not isinstance(item, str) for item in allowed):
        raise ContractFailure("allowed_without_diagnostics must be an array of strings")
    if not isinstance(forbidden, list) or any(not isinstance(item, str) for item in forbidden):
        raise ContractFailure("forbidden_fragments must be an array of strings")
    failures = []
    for warning in warnings:
        lowered = warning.lower()
        if not diagnostics_supplied:
            remaining = lowered
            allowed_match = False
            for fragment in allowed:
                normalized = fragment.lower()
                if normalized in remaining:
                    allowed_match = True
                    remaining = remaining.replace(normalized, "")
            if allowed_match and not any(fragment.lower() in remaining for fragment in forbidden):
                continue
        if any(fragment.lower() in lowered for fragment in forbidden):
            failures.append(f"forbidden collection-quality warning: {warning}")
    return failures


def validate(report: Any, contract: Any, diagnostics_supplied: bool) -> tuple[list[str], int]:
    if not isinstance(report, dict):
        raise ContractFailure("report JSON root must be an object")
    if not isinstance(contract, dict):
        raise ContractFailure("contract JSON root must be an object")
    unknown = set(contract) - TOP_LEVEL_KEYS
    if unknown:
        raise ContractFailure(f"contract has unsupported keys: {sorted(unknown)}")
    if contract.get("schema") != 1:
        raise ContractFailure(f"unsupported contract schema: {contract.get('schema')!r}")
    assertions = contract.get("assertions")
    relations = contract.get("relations", [])
    if not isinstance(assertions, list) or not assertions:
        raise ContractFailure("contract assertions must be a non-empty array")
    if not isinstance(relations, list):
        raise ContractFailure("contract relations must be an array")
    failures = []
    identifiers = []
    for assertion in assertions:
        if isinstance(assertion, dict):
            identifiers.append(assertion.get("id"))
        failures.extend(validate_assertion(report, assertion))
    for relation in relations:
        if isinstance(relation, dict):
            identifiers.append(relation.get("id"))
        failures.extend(validate_relation(report, relation))
    duplicate_ids = sorted({identifier for identifier in identifiers if identifiers.count(identifier) > 1})
    if duplicate_ids:
        raise ContractFailure(f"contract ids must be unique: {duplicate_ids}")
    failures.extend(validate_warnings(report, contract.get("warnings"), diagnostics_supplied))
    return failures, len(assertions) + len(relations)


def main() -> int:
    arguments = parse_arguments()
    try:
        report = read_json(Path(arguments.report), "report")
        contract = read_json(Path(arguments.contract), "contract")
        failures, checks = validate(report, contract, arguments.diagnostics_supplied)
    except ContractFailure as error:
        print(f"[jankhunter-e2e-contract] error: {error}", file=sys.stderr)
        return 1
    if failures:
        for failure in failures:
            print(f"[jankhunter-e2e-contract] error: {failure}", file=sys.stderr)
        return 1
    print(f"[jankhunter-e2e-contract] PASS: {checks} metric checks")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

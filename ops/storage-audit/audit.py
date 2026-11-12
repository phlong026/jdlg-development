#!/usr/bin/env python3
"""Offline, read-only audit of an exported object-storage manifest."""

from __future__ import annotations

import argparse
import csv
import heapq
import json
import os
import re
import sys
import tempfile
from collections import defaultdict
from pathlib import Path
from typing import Any, Iterable, Iterator, TextIO


KEY_FIELDS = ("key", "objectkey", "object", "name", "path")
SIZE_FIELDS = ("sizebytes", "size", "contentlength")
SHA256_FIELDS = ("sha256", "metadatasha256", "contentsha256", "xcosmetasha256")
MIME_FIELDS = ("contenttype", "mimetype", "mime")
STORAGE_CLASS_FIELDS = ("storageclass",)
MODIFIED_FIELDS = ("lastmodified", "lastmodifieddate", "modifiedat")
SHA256_RE = re.compile(r"^[0-9a-fA-F]{64}$")

TYPE_BY_EXTENSION = {
    "image": {"jpg", "jpeg", "png", "gif", "webp", "heic", "heif", "svg", "bmp", "tif", "tiff"},
    "video": {"mp4", "mov", "m4v", "webm", "mkv", "avi", "flv", "ts", "m3u8"},
    "audio": {"mp3", "m4a", "aac", "wav", "flac", "ogg", "opus"},
    "document": {"pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "csv", "json"},
    "archive": {"zip", "rar", "7z", "tar", "gz", "bz2", "xz"},
}


class AuditError(ValueError):
    """A manifest is invalid or exceeds an explicit resource limit."""


def _normalized_fields(record: dict[str, Any]) -> dict[str, Any]:
    return {re.sub(r"[^a-z0-9]", "", str(name).lower()): value for name, value in record.items()}


def _first(record: dict[str, Any], names: Iterable[str]) -> Any:
    for name in names:
        value = record.get(name)
        if value is not None and str(value).strip() != "":
            return value
    return None


def _format_for(path: Path, requested: str, *, references: bool = False) -> str:
    if requested != "auto":
        return requested
    suffix = path.suffix.lower()
    if suffix == ".csv":
        return "csv"
    if suffix in {".jsonl", ".ndjson"}:
        return "jsonl"
    if references:
        return "lines"
    raise AuditError(f"cannot infer manifest format from {path.name}; use --format csv or jsonl")


def _jsonl_records(stream: TextIO, source: Path) -> Iterator[tuple[int, dict[str, Any]]]:
    for line_number, line in enumerate(stream, 1):
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError as error:
            raise AuditError(f"{source.name}:{line_number}: invalid JSON: {error.msg}") from error
        if not isinstance(value, dict):
            raise AuditError(f"{source.name}:{line_number}: each JSONL line must be an object")
        yield line_number, value


def iter_records(path: Path, requested_format: str) -> Iterator[tuple[int, dict[str, Any]]]:
    actual_format = _format_for(path, requested_format)
    try:
        stream = path.open("r", encoding="utf-8-sig", newline="")
    except OSError as error:
        raise AuditError(f"cannot read {path}: {error}") from error
    with stream:
        if actual_format == "jsonl":
            yield from _jsonl_records(stream, path)
            return
        reader = csv.DictReader(stream)
        if not reader.fieldnames:
            raise AuditError(f"{path.name}: CSV header is missing")
        for line_number, record in enumerate(reader, 2):
            yield line_number, dict(record)


def load_references(path: Path | None, requested_format: str, max_references: int) -> set[str]:
    if path is None:
        return set()
    actual_format = _format_for(path, requested_format, references=True)
    references: set[str] = set()
    try:
        stream = path.open("r", encoding="utf-8-sig", newline="")
    except OSError as error:
        raise AuditError(f"cannot read {path}: {error}") from error
    with stream:
        if actual_format == "lines":
            values = ((line_number, line.strip()) for line_number, line in enumerate(stream, 1))
        elif actual_format == "jsonl":
            values = (
                (line_number, str(_first(_normalized_fields(record), KEY_FIELDS) or "").strip())
                for line_number, record in _jsonl_records(stream, path)
            )
        else:
            reader = csv.DictReader(stream)
            if not reader.fieldnames:
                raise AuditError(f"{path.name}: CSV header is missing")
            values = (
                (
                    line_number,
                    str(_first(_normalized_fields(dict(record)), KEY_FIELDS) or "").strip(),
                )
                for line_number, record in enumerate(reader, 2)
            )
        for line_number, key in values:
            if not key:
                raise AuditError(f"{path.name}:{line_number}: reference key is missing")
            references.add(key)
            if len(references) > max_references:
                raise AuditError(
                    f"reference limit exceeded ({max_references}); raise --max-references deliberately"
                )
    return references


def _normalized_object(record: dict[str, Any], source: Path, line_number: int) -> dict[str, Any]:
    fields = _normalized_fields(record)
    key = str(_first(fields, KEY_FIELDS) or "").strip()
    if not key:
        raise AuditError(f"{source.name}:{line_number}: object key is missing")
    raw_size = _first(fields, SIZE_FIELDS)
    try:
        size = int(str(raw_size).strip())
    except (TypeError, ValueError) as error:
        raise AuditError(f"{source.name}:{line_number}: size must be a non-negative integer") from error
    if size < 0:
        raise AuditError(f"{source.name}:{line_number}: size must be a non-negative integer")
    raw_sha256 = str(_first(fields, SHA256_FIELDS) or "").strip()
    sha256 = raw_sha256.lower() if SHA256_RE.fullmatch(raw_sha256) else None
    return {
        "key": key,
        "size_bytes": size,
        "sha256": sha256,
        "sha256_supplied": bool(raw_sha256),
        "content_type": str(_first(fields, MIME_FIELDS) or "").strip().lower(),
        "storage_class": str(_first(fields, STORAGE_CLASS_FIELDS) or "UNKNOWN").strip() or "UNKNOWN",
        "last_modified": str(_first(fields, MODIFIED_FIELDS) or "").strip(),
    }


def _object_type(key: str, content_type: str) -> str:
    media_type = content_type.split(";", 1)[0].strip()
    for category in ("image", "video", "audio"):
        if media_type.startswith(category + "/"):
            return category
    if media_type in {"application/pdf", "application/json"} or media_type.startswith("text/"):
        return "document"
    extension = key.rsplit("/", 1)[-1].rsplit(".", 1)[-1].lower() if "." in key.rsplit("/", 1)[-1] else ""
    for category, extensions in TYPE_BY_EXTENSION.items():
        if extension in extensions:
            return category
    return "other"


def _prefix(key: str, depth: int) -> str:
    components = [component for component in key.split("/") if component]
    if not components:
        return "(root)"
    if len(components) == 1:
        return "(root)"
    return "/".join(components[:depth]) + "/"


def _add_distribution(
    groups: dict[str, list[int]], name: str, size: int, max_groups: int, label: str
) -> None:
    if name not in groups and len(groups) >= max_groups:
        raise AuditError(f"{label} group limit exceeded ({max_groups}); raise --max-groups deliberately")
    stats = groups[name]
    stats[0] += 1
    stats[1] += size


def _distribution(groups: dict[str, list[int]]) -> list[dict[str, Any]]:
    rows = [
        {"name": name, "object_count": values[0], "bytes": values[1]}
        for name, values in groups.items()
    ]
    return sorted(rows, key=lambda row: (-row["bytes"], row["name"]))


def _push_top(heap: list[tuple[int, str, dict[str, Any]]], limit: int, row: dict[str, Any]) -> None:
    item = (row["size_bytes"], row["key"], row)
    if len(heap) < limit:
        heapq.heappush(heap, item)
    elif item[:2] > heap[0][:2]:
        heapq.heapreplace(heap, item)


def _sorted_top(heap: list[tuple[int, str, dict[str, Any]]]) -> list[dict[str, Any]]:
    return [item[2] for item in sorted(heap, key=lambda item: (-item[0], item[1]))]


def audit_manifest(
    manifest: Path,
    *,
    manifest_format: str = "auto",
    references: set[str] | None = None,
    references_supplied: bool = False,
    references_complete: bool = False,
    top: int = 50,
    prefix_depth: int = 1,
    max_hashes: int = 1_000_000,
    max_groups: int = 10_000,
    duplicate_samples: int = 5,
) -> dict[str, Any]:
    references = references or set()
    prefix_groups: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    type_groups: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    storage_groups: dict[str, list[int]] = defaultdict(lambda: [0, 0])
    largest: list[tuple[int, str, dict[str, Any]]] = []
    unreferenced_largest: list[tuple[int, str, dict[str, Any]]] = []
    # Each SHA keeps fixed-size samples. max_hashes makes total memory use explicit and bounded.
    hashes: dict[str, dict[str, Any]] = {}
    totals = {"object_count": 0, "bytes": 0}
    sha_stats = {"valid": 0, "missing": 0, "invalid": 0}
    unreferenced = {"object_count": 0, "bytes": 0}

    for line_number, raw_record in iter_records(manifest, manifest_format):
        item = _normalized_object(raw_record, manifest, line_number)
        key = item["key"]
        size = item["size_bytes"]
        totals["object_count"] += 1
        totals["bytes"] += size
        _add_distribution(prefix_groups, _prefix(key, prefix_depth), size, max_groups, "prefix")
        _add_distribution(type_groups, _object_type(key, item["content_type"]), size, max_groups, "type")
        _add_distribution(storage_groups, item["storage_class"], size, max_groups, "storage class")
        display = {
            "key": key,
            "size_bytes": size,
            "type": _object_type(key, item["content_type"]),
            "storage_class": item["storage_class"],
            "last_modified": item["last_modified"],
        }
        _push_top(largest, top, display)

        sha256 = item["sha256"]
        if sha256:
            sha_stats["valid"] += 1
            if sha256 not in hashes:
                if len(hashes) >= max_hashes:
                    raise AuditError(
                        f"SHA-256 group limit exceeded ({max_hashes}); "
                        "raise --max-hashes deliberately"
                    )
                hashes[sha256] = {
                    "object_count": 0,
                    "total_bytes": 0,
                    "first_size": size,
                    "size_consistent": True,
                    "sample_keys": [],
                }
            group = hashes[sha256]
            group["object_count"] += 1
            group["total_bytes"] += size
            group["size_consistent"] = group["size_consistent"] and size == group["first_size"]
            if len(group["sample_keys"]) < duplicate_samples:
                group["sample_keys"].append(key)
        elif item["sha256_supplied"]:
            sha_stats["invalid"] += 1
        else:
            sha_stats["missing"] += 1

        if references_supplied and key not in references:
            unreferenced["object_count"] += 1
            unreferenced["bytes"] += size
            _push_top(unreferenced_largest, top, display)

    duplicate_candidates: list[tuple[int, str, dict[str, Any]]] = []
    duplicate_group_count = 0
    duplicate_objects = 0
    estimated_reclaimable = 0
    inconsistent_groups = 0
    for sha256, group in hashes.items():
        if group["object_count"] < 2:
            continue
        duplicate_group_count += 1
        duplicate_objects += group["object_count"]
        if group["size_consistent"]:
            reclaimable = group["total_bytes"] - group["first_size"]
            estimated_reclaimable += reclaimable
        else:
            reclaimable = None
            inconsistent_groups += 1
        row = {
            "sha256": sha256,
            "object_count": group["object_count"],
            "total_bytes": group["total_bytes"],
            "potential_reclaimable_bytes": reclaimable,
            "size_consistent": group["size_consistent"],
            "sample_keys": group["sample_keys"],
        }
        rank = reclaimable if reclaimable is not None else 0
        candidate = (rank, sha256, row)
        if len(duplicate_candidates) < top:
            heapq.heappush(duplicate_candidates, candidate)
        elif candidate[:2] > duplicate_candidates[0][:2]:
            heapq.heapreplace(duplicate_candidates, candidate)
    top_duplicate_candidates = [
        item[2] for item in sorted(duplicate_candidates, key=lambda item: (-item[0], item[1]))
    ]

    warnings = [
        "ETag is ignored for duplicate detection; only a valid 64-hex SHA-256 field is accepted.",
        "All duplicate and unreferenced results are review candidates, never deletion instructions.",
    ]
    if sha_stats["missing"] or sha_stats["invalid"]:
        warnings.append("Objects without a valid SHA-256 were excluded from duplicate detection.")
    if inconsistent_groups:
        warnings.append("Duplicate SHA-256 groups with inconsistent sizes have no reclaimable-byte estimate.")
    if not references_supplied:
        warnings.append("No reference export was supplied; unreferenced candidates were not calculated.")
    elif not references_complete:
        warnings.append("Reference coverage is partial or unknown; absent keys are candidates only and may still be live.")
    else:
        warnings.append(
            "Even a declared-complete reference export can miss external or delayed consumers; "
            "verify before action."
        )

    return {
        "schema_version": 1,
        "safety": {
            "read_only": True,
            "network_access": False,
            "deletion_performed": False,
            "candidate_only": True,
            "reference_coverage": (
                "not_supplied"
                if not references_supplied
                else "declared_complete"
                if references_complete
                else "partial_or_unknown"
            ),
        },
        "limits": {
            "top_objects": top,
            "max_sha256_groups": max_hashes,
            "max_distribution_groups": max_groups,
            "duplicate_sample_keys_per_hash": duplicate_samples,
        },
        "totals": totals,
        "by_prefix": _distribution(prefix_groups),
        "by_type": _distribution(type_groups),
        "by_storage_class": _distribution(storage_groups),
        "largest_objects": _sorted_top(largest),
        "sha256_coverage": sha_stats,
        "duplicate_summary": {
            "candidate_groups": duplicate_group_count,
            "objects_in_candidate_groups": duplicate_objects,
            "potential_reclaimable_bytes_for_consistent_groups": estimated_reclaimable,
            "inconsistent_size_groups": inconsistent_groups,
        },
        "duplicate_candidates": top_duplicate_candidates,
        "duplicate_candidates_truncated": duplicate_group_count > len(top_duplicate_candidates),
        "unreferenced_summary": (
            {**unreferenced, "classification": "candidate_only"} if references_supplied else None
        ),
        "largest_unreferenced_candidates": _sorted_top(unreferenced_largest),
        "warnings": warnings,
    }


def _bounded_positive(value: str, name: str, maximum: int | None = None) -> int:
    try:
        parsed = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError(f"{name} must be an integer") from error
    if parsed < 1 or (maximum is not None and parsed > maximum):
        suffix = f" and at most {maximum}" if maximum is not None else ""
        raise argparse.ArgumentTypeError(f"{name} must be at least 1{suffix}")
    return parsed


def _write_json(report: dict[str, Any], output: Path | None) -> None:
    rendered = json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    if output is None:
        sys.stdout.write(rendered)
        return
    output.parent.mkdir(parents=True, exist_ok=True)
    file_descriptor, temporary_name = tempfile.mkstemp(prefix=f".{output.name}.", dir=output.parent)
    try:
        with os.fdopen(file_descriptor, "w", encoding="utf-8") as stream:
            stream.write(rendered)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary_name, output)
    except BaseException:
        try:
            os.unlink(temporary_name)
        except FileNotFoundError:
            pass
        raise


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path, help="local COS manifest exported as CSV or JSONL")
    parser.add_argument("--format", choices=("auto", "csv", "jsonl"), default="auto")
    parser.add_argument("--references", type=Path, help="optional known-reference export")
    parser.add_argument(
        "--references-format", choices=("auto", "lines", "csv", "jsonl"), default="auto"
    )
    parser.add_argument(
        "--references-complete",
        action="store_true",
        help="mark reference export as a complete scan claim; results remain candidates only",
    )
    parser.add_argument("--output", type=Path, help="write JSON report atomically instead of stdout")
    parser.add_argument("--top", type=lambda value: _bounded_positive(value, "top", 1000), default=50)
    parser.add_argument(
        "--prefix-depth", type=lambda value: _bounded_positive(value, "prefix-depth", 20), default=1
    )
    parser.add_argument(
        "--max-hashes", type=lambda value: _bounded_positive(value, "max-hashes"), default=1_000_000
    )
    parser.add_argument(
        "--max-references",
        type=lambda value: _bounded_positive(value, "max-references"),
        default=2_000_000,
    )
    parser.add_argument(
        "--max-groups", type=lambda value: _bounded_positive(value, "max-groups"), default=10_000
    )
    parser.add_argument(
        "--duplicate-samples",
        type=lambda value: _bounded_positive(value, "duplicate-samples", 100),
        default=5,
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    arguments = build_parser().parse_args(argv)
    try:
        if arguments.references_complete and arguments.references is None:
            raise AuditError("--references-complete requires --references")
        input_paths = {arguments.manifest.resolve()}
        if arguments.references is not None:
            input_paths.add(arguments.references.resolve())
        if arguments.output is not None and arguments.output.resolve() in input_paths:
            raise AuditError("output path must not overwrite an input file")
        references = load_references(
            arguments.references, arguments.references_format, arguments.max_references
        )
        report = audit_manifest(
            arguments.manifest,
            manifest_format=arguments.format,
            references=references,
            references_supplied=arguments.references is not None,
            references_complete=arguments.references_complete,
            top=arguments.top,
            prefix_depth=arguments.prefix_depth,
            max_hashes=arguments.max_hashes,
            max_groups=arguments.max_groups,
            duplicate_samples=arguments.duplicate_samples,
        )
        report["limits"]["max_references"] = arguments.max_references
        report["reference_key_count"] = len(references) if arguments.references is not None else None
        _write_json(report, arguments.output)
        return 0
    except (AuditError, OSError) as error:
        print(f"storage-audit: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

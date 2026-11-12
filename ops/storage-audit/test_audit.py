import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
SCRIPT = ROOT / "audit.py"
SPEC = importlib.util.spec_from_file_location("storage_audit", SCRIPT)
audit = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(audit)


class StorageAuditTest(unittest.TestCase):
    def write_jsonl(self, path: Path, rows: list[dict]) -> None:
        path.write_text("".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8")

    def test_reports_distribution_top_duplicates_and_partial_references(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest = root / "objects.jsonl"
            digest = "ab" * 32
            self.write_jsonl(
                manifest,
                [
                    {"key": "video/a.mp4", "size": 100, "sha256": digest, "storage_class": "STANDARD"},
                    {"key": "backup/a.mp4", "size": 100, "sha256": digest, "content_type": "video/mp4"},
                    {"key": "image/hero.jpg", "size": 250, "etag": digest, "content_type": "image/jpeg"},
                    {"key": "docs/readme.txt", "size": 10, "sha256": "not-a-sha"},
                ],
            )
            references = {"video/a.mp4"}

            report = audit.audit_manifest(
                manifest,
                references=references,
                references_supplied=True,
                top=2,
            )

            self.assertEqual(report["totals"], {"object_count": 4, "bytes": 460})
            self.assertEqual([row["size_bytes"] for row in report["largest_objects"]], [250, 100])
            self.assertEqual(report["duplicate_summary"]["candidate_groups"], 1)
            self.assertEqual(report["duplicate_summary"]["potential_reclaimable_bytes_for_consistent_groups"], 100)
            self.assertEqual(report["sha256_coverage"], {"valid": 2, "missing": 1, "invalid": 1})
            self.assertEqual(report["unreferenced_summary"]["object_count"], 3)
            self.assertEqual(report["safety"]["reference_coverage"], "partial_or_unknown")
            self.assertTrue(report["safety"]["candidate_only"])
            self.assertIn("ETag is ignored", report["warnings"][0])

    def test_duplicate_details_are_bounded_but_summary_is_complete(self):
        with tempfile.TemporaryDirectory() as temporary:
            manifest = Path(temporary) / "objects.jsonl"
            rows = []
            for index in range(3):
                digest = f"{index + 1:064x}"
                rows.extend(
                    [
                        {"key": f"{index}/a", "size": index + 1, "sha256": digest},
                        {"key": f"{index}/b", "size": index + 1, "sha256": digest},
                    ]
                )
            self.write_jsonl(manifest, rows)

            report = audit.audit_manifest(manifest, top=2)

            self.assertEqual(report["duplicate_summary"]["candidate_groups"], 3)
            self.assertEqual(len(report["duplicate_candidates"]), 2)
            self.assertTrue(report["duplicate_candidates_truncated"])

    def test_csv_aliases_and_declared_complete_references(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest = root / "objects.csv"
            manifest.write_text(
                "Key,Size,Content-Type,StorageClass,ETag\n"
                "public/banner.png,20,image/png,STANDARD,aaa\n"
                "archive/file.zip,30,application/zip,ARCHIVE,bbb\n",
                encoding="utf-8",
            )
            refs = root / "references.csv"
            refs.write_text("ObjectKey\npublic/banner.png\n", encoding="utf-8")
            known = audit.load_references(refs, "auto", 10)

            report = audit.audit_manifest(
                manifest,
                references=known,
                references_supplied=True,
                references_complete=True,
            )

            self.assertEqual(report["safety"]["reference_coverage"], "declared_complete")
            self.assertEqual(report["duplicate_summary"]["candidate_groups"], 0)
            self.assertEqual(report["unreferenced_summary"], {
                "object_count": 1,
                "bytes": 30,
                "classification": "candidate_only",
            })
            self.assertEqual({row["name"] for row in report["by_type"]}, {"image", "archive"})

    def test_rejects_invalid_size_with_source_line(self):
        with tempfile.TemporaryDirectory() as temporary:
            manifest = Path(temporary) / "objects.jsonl"
            self.write_jsonl(manifest, [{"key": "bad", "size": "1.5"}])
            with self.assertRaisesRegex(audit.AuditError, r"objects\.jsonl:1"):
                audit.audit_manifest(manifest)

    def test_enforces_hash_and_reference_limits(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest = root / "objects.jsonl"
            self.write_jsonl(
                manifest,
                [
                    {"key": "a", "size": 1, "sha256": "01" * 32},
                    {"key": "b", "size": 1, "sha256": "02" * 32},
                ],
            )
            with self.assertRaisesRegex(audit.AuditError, "SHA-256 group limit"):
                audit.audit_manifest(manifest, max_hashes=1)
            refs = root / "refs.txt"
            refs.write_text("a\nb\n", encoding="utf-8")
            with self.assertRaisesRegex(audit.AuditError, "reference limit"):
                audit.load_references(refs, "auto", 1)

    def test_cli_writes_report_and_never_overwrites_input(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            manifest = root / "objects.jsonl"
            self.write_jsonl(manifest, [{"key": "a.jpg", "size": 12}])
            report = root / "report.json"
            completed = subprocess.run(
                [sys.executable, str(SCRIPT), str(manifest), "--output", str(report)],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertTrue(json.loads(report.read_text(encoding="utf-8"))["safety"]["read_only"])
            rejected = subprocess.run(
                [sys.executable, str(SCRIPT), str(manifest), "--output", str(manifest)],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(rejected.returncode, 2)
            self.assertIn("must not overwrite", rejected.stderr)


if __name__ == "__main__":
    unittest.main()

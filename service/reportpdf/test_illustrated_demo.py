"""The illustrated sample must keep the customer's confirmed input intact."""
import copy
import json
from pathlib import Path
import unittest

from build_illustrated_demo import illustrated_snapshot


class IllustratedDemoTests(unittest.TestCase):
    def setUp(self):
        source = Path(__file__).resolve().parent.parent / "report/testdata/v5-demo/space-full.json"
        self.original = json.loads(source.read_text(encoding="utf-8"))

    def test_keeps_facts_confirmation_and_provenance(self):
        before = copy.deepcopy(self.original)
        result = illustrated_snapshot(self.original)
        self.assertEqual(self.original, before)
        for key in ("facts", "confirmation", "diagnosis"):
            self.assertEqual(result[key], before[key])
        self.assertEqual(result["meta"]["input_hash"], before["meta"]["input_hash"])
        self.assertTrue(result["meta"]["demonstration"])

    def test_ten_pages_with_both_states_of_selected_direction(self):
        result = illustrated_snapshot(self.original)
        self.assertEqual([p["number"] for p in result["pages"]], list(range(1, 11)))
        self.assertEqual([p["kind"] for p in result["pages"]], [p["kind"] for p in self.original["pages"]])
        self.assertEqual([i["asset"] for i in result["pages"][4]["illustrations"]], ["flex-day.png", "flex-night.png"])
        self.assertIn("固定客房", result["pages"][3]["title"])
        self.assertGreaterEqual(sum(len(p.get("illustrations", [])) for p in result["pages"]), 7)

    def test_rejects_different_customer_or_choice(self):
        for key, value in (("choice", "option_a"), ("choice", "undecided")):
            changed = copy.deepcopy(self.original)
            changed["confirmation"][key] = value
            with self.assertRaises(ValueError):
                illustrated_snapshot(changed)
        changed = copy.deepcopy(self.original)
        changed["facts"]["household.permanent_count"] = "6"
        with self.assertRaises(ValueError):
            illustrated_snapshot(changed)


if __name__ == "__main__":
    unittest.main()

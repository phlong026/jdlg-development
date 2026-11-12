"""Offline regression checks; no credentials or production database access."""
import importlib.util
import json
import os
import subprocess
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]

class PromptCharsetTests(unittest.TestCase):
    def test_migration_and_import_pin_connection_charset(self):
        sql = (ROOT / 'service/sql/phase3_writing_prompts_up.sql').read_text()
        self.assertIn('SET NAMES utf8mb4;', sql)
        script = (ROOT / 'ops/b18a-deployment-checklist.sh').read_text()
        self.assertIn('mysql --default-character-set=utf8mb4', script)

    def module(self):
        path = ROOT / 'ops/plan_writing_prompt_repair.py'
        spec = importlib.util.spec_from_file_location('repair', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_repair_only_exact_corrupted_seed_fields(self):
        m = self.module()
        seeds = m.read_seeds()
        self.assertEqual(set(seeds), {'design', 'project', 'promotion'})
        row = dict(seeds['design'], prompt_key='design', id=7, updated_by='seed', sort_order=1, enabled=1, updated_at='2026-11-02 12:00:00.123456')
        row['label'] = m.mojibake(row['label'])
        row['instruction'] = '运营调整后的正常指令，不能覆盖。'
        patches = m.plan([row])
        self.assertEqual(patches[0]['changes'], {'label': '设计说明'})
        self.assertEqual(patches[0]['before']['instruction'], row['instruction'])
        apply, rollback = m.render(patches)
        self.assertIn('BINARY `instruction`', apply)
        self.assertIn('BINARY `label`', rollback)
        self.assertIn('updated_at', apply)
        self.assertNotIn('运营', apply)  # SQL literals are UTF-8 hex, never shell interpolation.

    def test_normal_custom_unknown_and_unrecoverable_rows_are_untouched(self):
        m = self.module()
        normal = dict(m.read_seeds()['design'], prompt_key='design', id=1, updated_by='seed', sort_order=1, enabled=1, updated_at='2026-11-02 12:00:00')
        custom = dict(normal, label='我的专属写作', instruction='保留运营指令')
        unknown = dict(normal, prompt_key='custom', label=m.mojibake(normal['label']))
        broken = dict(normal, label='????')
        for row in (normal, custom, unknown, broken):
            self.assertEqual(m.plan([row]), [])

    def test_repair_is_idempotent_and_rejects_incomplete_snapshot(self):
        m = self.module()
        row = dict(m.read_seeds()['project'], prompt_key='project', id=2, updated_by='seed', sort_order=2, enabled=1, updated_at='2026-11-02 12:00:00')
        row['heading'] = m.mojibake(row['heading'])
        patch = m.plan([row])[0]
        self.assertEqual(m.plan([dict(row, **patch['changes'])]), [])
        del row['updated_at']
        with self.assertRaises(ValueError):
            m.plan([row])

@unittest.skipUnless(os.environ.get('PROMPT_TEST_CONTAINER'), 'Set PROMPT_TEST_CONTAINER to an isolated MySQL test container')
class MySQLPromptCharsetTests(unittest.TestCase):
    module = PromptCharsetTests.module

    def sql(self, query, charset='utf8mb4'):
        result = subprocess.run(
            ['docker', 'exec', '-i', os.environ['PROMPT_TEST_CONTAINER'], 'mysql',
             '-uroot', '--default-character-set=' + charset, '--batch', '--raw', '--skip-column-names'],
            input=query, text=True, encoding='utf-8', capture_output=True, timeout=30, check=True)
        return result.stdout

    def test_real_mysql_corruption_repair_rollback_and_stale_snapshot(self):
        m = self.module()
        migration = (ROOT / 'service/sql/phase3_writing_prompts_up.sql').read_text()
        snapshot_sql = (ROOT / 'service/sql/phase3_writing_prompts_snapshot.sql').read_text()
        database = 'prompt_charset_regression'
        prefix = 'USE ' + database + ';\n'
        self.sql('DROP DATABASE IF EXISTS ' + database + '; CREATE DATABASE ' + database + ';')
        try:
            # Reproduce the old migration under a latin1 connection; table stays utf8mb4.
            self.sql(prefix + migration.replace('SET NAMES utf8mb4;', ''), charset='latin1')
            raw = self.sql(prefix + snapshot_sql)
            rows = [json.loads(line) for line in raw.splitlines()]
            self.assertEqual(rows[0]['label'], m.mojibake('设计说明'))
            self.assertEqual(len(m.plan(rows)), 3)
            # An operator's healthy instruction must survive even when its row label is damaged.
            self.sql(prefix + "UPDATE content_writing_prompts SET instruction='运营定制内容' WHERE prompt_key='design';")
            rows = [json.loads(line) for line in self.sql(prefix + snapshot_sql).splitlines()]
            apply, rollback = m.render(m.plan(rows))
            self.assertEqual(self.sql(prefix + apply).split(), ['1', '1', '1'])
            repaired = [json.loads(line) for line in self.sql(prefix + snapshot_sql).splitlines()]
            self.assertEqual(repaired[0]['label'], '设计说明')
            self.assertEqual(repaired[0]['instruction'], '运营定制内容')
            self.assertEqual(m.plan(repaired), [])
            self.assertEqual(self.sql(prefix + apply).split(), ['0', '0', '0'])
            self.assertEqual(self.sql(prefix + rollback).split(), ['1', '1', '1'])
            restored = [json.loads(line) for line in self.sql(prefix + snapshot_sql).splitlines()]
            self.assertEqual(restored, rows)
            # A later operator edit blocks repair of that row, rather than overwriting it.
            self.sql(prefix + "UPDATE content_writing_prompts SET instruction='后续运营编辑' WHERE prompt_key='design';")
            self.assertEqual(self.sql(prefix + apply).split(), ['0', '1', '1'])
            self.sql(prefix + "UPDATE content_writing_prompts SET label='保留后续名称' WHERE prompt_key='project';")
            self.assertEqual(self.sql(prefix + rollback).split(), ['0', '0', '1'])
            # New migration self-identifies SQL source encoding even with legacy client settings.
            self.sql(prefix + 'DROP TABLE content_writing_prompts;')
            self.sql(prefix + migration, charset='latin1')
            clean = [json.loads(line) for line in self.sql(prefix + snapshot_sql).splitlines()]
            self.assertEqual(clean[0]['label'], '设计说明')
            self.assertEqual(m.plan(clean), [])
        finally:
            self.sql('DROP DATABASE IF EXISTS ' + database + ';')


if __name__ == '__main__':
    unittest.main()

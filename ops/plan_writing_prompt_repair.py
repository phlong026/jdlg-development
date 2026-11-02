#!/usr/bin/env python3
"""Generate reviewable SQL from a read-only JSONL snapshot; never connect/write DB.

Export using service/sql/phase3_writing_prompts_snapshot.sql with mysql
--default-character-set=utf8mb4 --batch --raw --skip-column-names, using the
operator's existing login-path/defaults file (do not put credentials in commands).
Then: python3 ops/plan_writing_prompt_repair.py snapshot.jsonl NEW_OUTPUT_DIR
Review plan.json/apply.sql before applying. Keep rollback.sql and the snapshot.
Only exact latin1/cp1252 corruption of shipped seed fields is repaired. Unknown
or lossy corruption needs manual review. SQL uses compare-and-swap predicates:
changed rows are skipped, including during rollback. Check affected_rows; zero
means stale/already-applied and must not be reported as a successful repair.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
FIELDS = ('label', 'heading', 'example', 'instruction')
GUARD_FIELDS = ('prompt_key', *FIELDS, 'updated_at', 'updated_by', 'sort_order', 'enabled')


def read_seeds():
    source = (ROOT / 'service/sql/phase3_writing_prompts_up.sql').read_text(encoding='utf-8')
    # Parse only this migration's fixed seed tuple format; fail closed if it changes.
    pattern = r"\('([^']+)', '([^']+)', '([^']+)',\s*'([^']+)',\s*'([^']+)', \d+, \d+, 'seed'\)"
    rows = re.findall(pattern, source)
    if len(rows) != 3 or {row[0] for row in rows} != {'design', 'project', 'promotion'}:
        raise ValueError('Seed SQL format changed; review the repair planner before use')
    return {row[0]: dict(zip(FIELDS, row[1:])) for row in rows}


def mojibake(value):
    # MySQL latin1 maps defined C1 bytes as cp1252 and preserves undefined controls.
    return ''.join(bytes([byte]).decode('cp1252') if byte not in (0x81, 0x8d, 0x8f, 0x90, 0x9d)
                   else chr(byte) for byte in value.encode('utf-8'))


def plan(rows):
    seeds = read_seeds()
    patches = []
    seen = set()
    for row in rows:
        if not all(key in row for key in ('id', *GUARD_FIELDS)):
            raise ValueError('Snapshot is incomplete; export with the supplied snapshot SQL')
        if not isinstance(row['id'], int) or row['id'] <= 0 or row['id'] in seen:
            raise ValueError('Snapshot contains an invalid or duplicate row id')
        seen.add(row['id'])
        expected = seeds.get(row['prompt_key'], {})
        changes = {field: value for field, value in expected.items()
                   if row[field] != value and row[field] in (mojibake(value), value.encode('utf-8').decode('latin1'))}
        if changes:
            patches.append({'before': row, 'changes': changes})
    return patches


def literal(value):
    if isinstance(value, int):
        return str(value)
    if not isinstance(value, str):
        raise ValueError('Snapshot contains an unsupported value')
    return "CONVERT(X'" + value.encode('utf-8').hex() + "' USING utf8mb4)"


def update_sql(before, changes):
    assignments = ', '.join(f'`{key}` = {literal(value)}' for key, value in changes.items())
    predicates = [f'`id` = {before["id"]}']
    predicates += [f'BINARY `{key}` = BINARY {literal(before[key])}' for key in GUARD_FIELDS]
    return ('UPDATE content_writing_prompts SET ' + assignments + '\nWHERE '
            + '\n  AND '.join(predicates) + ';\nSELECT ROW_COUNT() AS affected_rows;')


def render(patches):
    apply, rollback = [], []
    repaired_at = datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S.%f')
    for patch in patches:
        before, changes = patch['before'], dict(patch['changes'], updated_at=repaired_at)
        after = dict(before, **changes)
        apply.append(update_sql(before, changes))
        rollback.append(update_sql(after, {key: before[key] for key in changes}))
    def transaction(statements):
        return 'SET NAMES utf8mb4;\nSET SESSION innodb_lock_wait_timeout = 10;\nSTART TRANSACTION;\n' + '\n'.join(statements) + '\nCOMMIT;\n'
    return transaction(apply), transaction(rollback)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('snapshot', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    try:
        snapshot = args.snapshot.read_text(encoding='utf-8')
        rows = [json.loads(line) for line in snapshot.splitlines() if line.strip()]
        patches = plan(rows)
        apply, rollback = render(patches)
        args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
        for name, value in (('snapshot.jsonl', snapshot), ('plan.json', json.dumps(patches, ensure_ascii=False, indent=2)),
                            ('apply.sql', apply), ('rollback.sql', rollback)):
            (args.output / name).write_text(value, encoding='utf-8')
        print(f'Planned {len(patches)} row(s). SQL generated only; no database writes performed.')
    except (OSError, ValueError, TypeError, KeyError) as error:
        print(f'Repair plan failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

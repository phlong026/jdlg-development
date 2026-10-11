"""Keep pagination and free/paid snapshots stable across visual changes."""
import json
import os
from pathlib import Path
import tempfile
import unittest

from pypdf import PdfReader
from render import render_report

HERE = Path(__file__).resolve().parent
FONT = Path(os.environ.get('REPORT_PDF_FONT', '/System/Library/Fonts/Supplemental/Arial Unicode.ttf'))


class ReportTests(unittest.TestCase):
    def setUp(self):
        self.data = json.loads((HERE / 'sample.json').read_text())
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name) / 'report.pdf'

    def render(self, mode='full', data=None):
        render_report(data or self.data, self.output, mode, FONT)
        return PdfReader(self.output)

    def test_preview_contains_only_four_complete_a4_pages(self):
        full = self.render()
        full_text = [p.extract_text() for p in full.pages]
        preview = self.render('preview')
        self.assertEqual(len(full.pages), 14)
        self.assertEqual(len(preview.pages), 4)
        for i, page in enumerate(preview.pages):
            self.assertEqual(page.extract_text(), full_text[i])
            self.assertEqual(page.get_contents().get_data(), full.pages[i].get_contents().get_data())
            self.assertAlmostEqual(float(page.mediabox.width), 595.28, places=1)
            self.assertAlmostEqual(float(page.mediabox.height), 841.89, places=1)
        self.assertNotIn('方案 A｜', ''.join(p.extract_text() for p in preview.pages))

    def test_preview_does_not_need_paid_content_or_plans(self):
        self.data['pages'] = self.data['pages'][:4]
        del self.data['schemes']
        del self.data['budget']
        self.assertEqual(len(self.render('preview').pages), 4)

    def test_changed_user_fields_are_not_replaced_by_sample_facts(self):
        self.data['facts']['region'] = '浙江 · 湖州'
        self.data['facts']['budget_wan'] = 90
        self.data['facts']['goal'] = '让两代人的生活彼此照顾，也各有空间'
        text = '\n'.join(p.extract_text() for p in self.render('preview').pages)
        self.assertIn('浙江', text)
        self.assertIn('90', text)
        self.assertNotIn('120 万元', text)
        self.assertNotIn('120万元', text)
        self.assertNotIn('福建泉州晋江', text)

    def test_bad_order_and_missing_pages_are_rejected(self):
        self.data['pages'][1], self.data['pages'][2] = self.data['pages'][2], self.data['pages'][1]
        with self.assertRaises(ValueError):
            self.render()

    def test_overlong_copy_fails_without_publishing_partial_pdf(self):
        self.data['pages'][1]['blocks'][0]['body'] = '超长内容' * 800
        with self.assertRaises(ValueError):
            self.render()
        self.assertFalse(self.output.exists())

    def test_failed_rerender_keeps_the_previous_valid_file(self):
        self.render('preview')
        original = self.output.read_bytes()
        self.data['pages'][1]['blocks'][0]['body'] = '超长内容' * 800
        with self.assertRaises(ValueError):
            self.render('preview')
        self.assertEqual(self.output.read_bytes(), original)

    def test_same_snapshot_renders_identical_pdf_bytes(self):
        self.render()
        original = self.output.read_bytes()
        self.render()
        self.assertEqual(self.output.read_bytes(), original)

    def test_unreviewed_schemes_cannot_become_customer_deliverables(self):
        self.data['meta']['demonstration'] = False
        for mode in ('preview', 'full'):
            with self.assertRaises(ValueError):
                self.render(mode)

    def test_extra_table_cell_is_not_silently_dropped(self):
        self.data['pages'][11]['table'][1].append('SHOULD_NOT_DISAPPEAR')
        with self.assertRaises(ValueError):
            self.render()

    def test_extra_table_rows_and_actions_cannot_overlap_following_sections(self):
        for field, index in [('table', 11), ('actions', 13)]:
            with self.subTest(field=field):
                self.data['pages'][index][field].append(self.data['pages'][index][field][0])
                with self.assertRaises(ValueError):
                    self.render()
                self.data['pages'][index][field].pop()

    def test_extra_copy_blocks_are_rejected_instead_of_ignored(self):
        self.data['pages'][2]['blocks'].append({'title': '多余', 'body': '不应丢失'})
        with self.assertRaises(ValueError):
            self.render('preview')

    def test_page_titles_and_chinese_are_searchable(self):
        reader = self.render()
        for p, data in zip(reader.pages, self.data['pages']):
            self.assertIn(data['title'], p.extract_text())
            self.assertIn('非施工图', p.extract_text())

    def test_invalid_mode_rejected(self):
        with self.assertRaises(ValueError):
            self.render('unlocked')

    def test_floor_heading_cannot_overlap_its_description(self):
        self.data['schemes'][0]['floors'][0]['name'] = '楼层名称' * 8
        with self.assertRaises(ValueError):
            self.render()
        self.assertFalse(self.output.exists())

    def test_real_preview_requires_service_provenance_and_omits_demo_house(self):
        self.data['meta'].update(demonstration=False, source='report-service-v1',
                                 protocol_version='report-service-v1', input_hash='a' * 64)
        self.data['pages'] = self.data['pages'][:4]
        self.data.pop('schemes')
        self.data.pop('budget')
        reader = self.render('preview')
        text = '\n'.join(page.extract_text() for page in reader.pages)
        self.assertNotIn('虚构家庭样稿', text)
        self.assertNotIn('所选风格参考', text)
        self.assertEqual(len(reader.pages), 4)

    def test_real_asset_paths_cannot_escape_the_trusted_root(self):
        self.data['meta'].update(demonstration=False, source='report-service-v1',
                                 protocol_version='report-service-v1', input_hash='a' * 64)
        self.data['pages'] = self.data['pages'][:4]
        self.data['assets'] = {'bad': '/etc/passwd'}
        with self.assertRaises(ValueError):
            render_report(self.data, self.output, 'preview', FONT, asset_root=HERE / 'assets')


if __name__ == '__main__':
    unittest.main()

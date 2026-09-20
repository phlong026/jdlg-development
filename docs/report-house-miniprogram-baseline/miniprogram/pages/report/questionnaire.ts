import { emptyInput, validateStep, ReportInput } from './flow';
import { reportRequest } from './api';
import { requireReportLogin } from '../../utils/reportNavigation';
const DRAFT_KEY = 'building_report_draft_v1';
Page({
    data: {
        step: 0, input: emptyInput(), error: '', busy: false, consent: false, nextLabel: '继续，想清楚下一步',
        titles: ['这块地，在哪里？', '这栋房，谁来住？', '未来的日子，需要哪些空间？', '对这个家，您最在意什么？', '预算要花到哪里？'],
        families: ['三代同堂', '夫妻自住', '两代同住', '返乡度假'], styles: ['新闽派', '新中式', '现代简约', '欧式'],
        priorityOptions: ['老人起居', '家庭团聚', '采光通风', '庭院生活', '外观体面', '预算控制'],
        priorityChoices: [] as {
            label: string;
            checked: boolean;
        }[],
        scopes: ['主体毛坯', '主体及外装', '含室内装修'], floors: [1, 2, 3],
    },
    onLoad() {
        const draft = wx.getStorageSync(DRAFT_KEY);
        if (draft?.expires > Date.now() && draft.input?.site && draft.input?.household && draft.input?.space && Array.isArray(draft.input?.preference?.priorities) && draft.input?.budget) {
            const step = Math.max(0, Math.min(4, draft.step || 0));
            this.setData({ input: draft.input, step, nextLabel: step < 4 ? '继续，想清楚下一步' : '生成我家的建房方向' });
        }
        this.syncPriorityChoices();
    },
    onUnload() { this.saveDraft(); },
    saveDraft() { if (!(this as any)._submitted)
        wx.setStorageSync(DRAFT_KEY, { input: this.data.input, step: this.data.step, expires: Date.now() + 7 * 86400000 }); },
    field(e: any) {
        const key = e.currentTarget.dataset.field as string;
        const allowed = ['site.width_m', 'site.depth_m', 'household.residents', 'space.bedrooms', 'preference.goal', 'budget.wan'];
        if (!allowed.includes(key))
            return;
        const numeric = ['site.width_m', 'site.depth_m', 'space.bedrooms', 'budget.wan'].includes(key);
        this.setData({ ['input.' + key]: numeric ? Number(e.detail.value) : String(e.detail.value), error: '' });
        this.saveDraft();
    },
    region(e: any) { this.setData({ 'input.site.region': e.detail.value.join(' '), error: '' }); this.saveDraft(); },
    choice(e: any) {
        const { field, value } = e.currentTarget.dataset;
        if (!['household.family_type', 'space.floors', 'preference.style', 'budget.scope'].includes(field))
            return;
        this.setData({ ['input.' + field]: field === 'space.floors' ? Number(value) : value, error: '' });
        this.saveDraft();
    },
    syncPriorityChoices() { this.setData({ priorityChoices: this.data.priorityOptions.map(label => ({ label, checked: this.data.input.preference.priorities.includes(label) })) }); },
    priorities(e: any) {
        const values: string[] = e.detail.value.filter((value: string) => this.data.priorityOptions.includes(value));
        const previous = this.data.input.preference.priorities;
        const ordered = previous.filter(value => values.includes(value)).concat(values.filter(value => !previous.includes(value)));
        this.setData({ 'input.preference.priorities': ordered.slice(0, 3), error: ordered.length > 3 ? '最多选择三项；取消一项后可重新选择。' : '' });
        this.syncPriorityChoices();
        this.saveDraft();
    },
    elder(e: any) { this.setData({ 'input.space.first_floor_elder_room': e.detail.value }); this.saveDraft(); },
    consent(e: any) { this.setData({ consent: e.detail.value.length === 1 }); },
    previous() { const step = Math.max(0, this.data.step - 1); this.setData({ step, nextLabel: step < 4 ? '继续，想清楚下一步' : '生成我家的建房方向', error: '' }); wx.pageScrollTo({ scrollTop: 0, duration: 0 }); },
    async next() {
        if (this.data.busy)
            return;
        const error = validateStep(this.data.input as ReportInput, this.data.step);
        if (error) {
            this.setData({ error });
            return;
        }
        if (this.data.step < 4) {
            const step = this.data.step + 1;
            this.setData({ step, nextLabel: step < 4 ? '继续，想清楚下一步' : '生成我家的建房方向', error: '' });
            this.saveDraft();
            wx.pageScrollTo({ scrollTop: 0, duration: 0 });
            return;
        }
        for (let step = 0; step < 5; step++) {
            const message = validateStep(this.data.input, step);
            if (message) {
                this.setData({ step, nextLabel: step < 4 ? '继续，想清楚下一步' : '生成我家的建房方向', error: message });
                return;
            }
        }
        if (!this.data.consent) {
            this.setData({ error: '请确认同意将这些信息用于生成您的建房报告。' });
            return;
        }
        this.saveDraft();
        if (!requireReportLogin('/pages/report/questionnaire'))
            return;
        this.setData({ busy: true, error: '' });
        try {
            const result = await reportRequest('', 'POST', this.data.input);
            (this as any)._submitted = true;
            wx.removeStorageSync(DRAFT_KEY);
            wx.redirectTo({ url: '/pages/report/preview?id=' + encodeURIComponent(result.id), fail: () => { (this as any)._submitted = false; this.setData({ error: '报告已创建，打开失败；再次提交会回到同一份报告。' }); } });
        }
        catch (error: any) {
            this.setData({ error: error.message });
        }
        finally {
            this.setData({ busy: false });
        }
    },
});

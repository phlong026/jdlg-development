import { emptyInput, validateStep, emptyHouseDetails, validateHouseStep, houseRequestInput } from './flow';
import { reportRequest } from './api';
import { requireReportLogin } from '../../utils/reportNavigation';
const DRAFT_KEY = 'building_report_draft_v1';
const HOUSE_DRAFT_KEY = 'building_report_draft_house_v1';
Page({
    data: {
        step: 0, input: emptyInput(), error: '', busy: false, consent: false, nextLabel: '继续，想清楚下一步',
        houseEnabled: false, house: emptyHouseDetails(), configReady: false, configLoading: true,
        frequencies: [{ value: 'none', label: '没有返乡成员' }, { value: 'monthly', label: '每月' }, { value: 'quarterly', label: '每季度' }, { value: 'holidays', label: '节假日' }, { value: 'yearly', label: '每年' }, { value: 'unknown', label: '尚不确定' }],
        yesNo: [{ value: 'yes', label: '需要' }, { value: 'no', label: '不需要' }, { value: 'unknown', label: '待比较' }],
        scopeStatuses: [{ value: 'included', label: '包含' }, { value: 'excluded', label: '不包含' }, { value: 'unknown', label: '待确认' }],
        titles: ['这块地，在哪里？', '这栋房，谁来住？', '未来的日子，需要哪些空间？', '对这个家，您最在意什么？', '预算要花到哪里？'],
        families: ['三代同堂', '夫妻自住', '两代同住', '返乡度假'], styles: ['新闽派', '新中式', '现代简约', '欧式'],
        priorityOptions: ['老人起居', '家庭团聚', '采光通风', '庭院生活', '外观体面', '预算控制'],
        priorityChoices: [] as {
            label: string;
            checked: boolean;
        }[],
        scopes: ['主体毛坯', '主体及外装', '含室内装修'], floors: [1, 2, 3],
    },
    async onLoad() {
        this.syncPriorityChoices();
        await this.loadConfig();
    },
    async loadConfig() {
        this.setData({ configReady: false, configLoading: true, error: '' });
        try {
            const config = await reportRequest('/config');
            if (!config.enabled) throw new Error('建房报告服务正在准备中，请稍后再来。');
            if (config.house_enabled) {
                const schema = await reportRequest('/house/questions');
                if (schema.version !== 'report-house-v1') throw new Error('整屋报告问卷暂时不可用，请稍后重试。');
            }
            this.setData({ houseEnabled: config.house_enabled === true, configReady: true });
            this.restoreDraft();
        } catch (error: any) { this.setData({ error: error.message }); }
        finally { this.setData({ configLoading: false }); }
    },
    restoreDraft() {
        const draft = wx.getStorageSync(this.data.houseEnabled ? HOUSE_DRAFT_KEY : DRAFT_KEY);
        if (draft?.expires > Date.now() && draft.input?.site && draft.input?.household && draft.input?.space && Array.isArray(draft.input?.preference?.priorities) && draft.input?.budget) {
            const step = Math.max(0, Math.min(4, draft.step || 0));
            const house = { ...emptyHouseDetails(), ...(draft.house || {}) };
            if (!Array.isArray(house.rooms) || !Array.isArray(house.budget_scope)) return;
            this.setData({ input: draft.input, house, step, nextLabel: step < 4 ? '继续，想清楚下一步' : '生成我家的建房方向' });
        }
        this.syncPriorityChoices();
    },
    onUnload() { this.saveDraft(); },
    saveDraft() {
        if (!(this as any)._submitted && this.data.configReady)
            wx.setStorageSync(this.data.houseEnabled ? HOUSE_DRAFT_KEY : DRAFT_KEY, { input: this.data.input, house: this.data.house, step: this.data.step, expires: Date.now() + 7 * 86400000 });
    },
    field(e: any) {
        const key = e.currentTarget.dataset.field as string;
        const allowed = ['site.width_m', 'site.depth_m', 'site.road_side', 'household.residents', 'space.bedrooms', 'preference.goal', 'budget.wan'];
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
    houseField(e: any) {
        const field = e.currentTarget.dataset.field;
        if (!['permanent_count', 'returning_count', 'buildable_constraints', 'orientation', 'neighbor_notes', 'start_window'].includes(field)) return;
        const value = ['permanent_count', 'returning_count'].includes(field) ? (e.detail.value === '' ? null : Number(e.detail.value)) : String(e.detail.value);
        this.setData({ ['house.' + field]: value, error: '' }); this.saveDraft();
    },
    houseChoice(e: any) {
        const { field, value } = e.currentTarget.dataset;
        const allowed: { [key: string]: string[] } = { return_frequency: this.data.frequencies.map(item => item.value), buildable_scope: ['known', 'unknown'], flexible_use: ['yes', 'no', 'unknown'], elder_room: ['yes', 'no', 'unknown'], courtyard: ['yes', 'no', 'unknown'], parking: ['yes', 'no', 'unknown'] };
        if (!allowed[field]?.includes(value)) return;
        this.setData({ ['house.' + field]: value, error: '' }); this.saveDraft();
    },
    addRoom() {
        if (this.data.house.rooms.length >= 12) return;
        const rooms = this.data.house.rooms.slice();
        let next = 1; while (rooms.some(room => room.id === 'room-' + next)) next++;
        rooms.push({ id: 'room-' + next, name: '', count: 1, preferred_floor: 0, required: true });
        this.setData({ 'house.rooms': rooms }); this.saveDraft();
    },
    roomField(e: any) {
        const index = Number(e.currentTarget.dataset.index), field = e.currentTarget.dataset.field;
        if (!Number.isInteger(index) || !this.data.house.rooms[index] || !['name', 'count', 'preferred_floor', 'required'].includes(field)) return;
        const rooms = this.data.house.rooms.map(room => ({ ...room }));
        (rooms[index] as any)[field] = field === 'name' ? String(e.detail.value) : field === 'required' ? Boolean(e.detail.value) : Number(e.detail.value);
        this.setData({ 'house.rooms': rooms, error: '' }); this.saveDraft();
    },
    removeRoom(e: any) { const index = Number(e.currentTarget.dataset.index); this.setData({ 'house.rooms': this.data.house.rooms.filter((_, i) => i !== index) }); this.saveDraft(); },
    budgetScope(e: any) {
        const index = Number(e.currentTarget.dataset.index), status = e.currentTarget.dataset.status;
        if (!this.data.house.budget_scope[index] || !this.data.scopeStatuses.some(item => item.value === status)) return;
        const items = this.data.house.budget_scope.map(item => ({ ...item })); items[index].status = status;
        this.setData({ 'house.budget_scope': items }); this.saveDraft();
    },
    consent(e: any) { this.setData({ consent: e.detail.value.length === 1 }); },
    previous() { const step = Math.max(0, this.data.step - 1); this.setData({ step, nextLabel: step < 4 ? '继续，想清楚下一步' : '生成我家的建房方向', error: '' }); wx.pageScrollTo({ scrollTop: 0, duration: 0 }); },
    async next() {
        if (this.data.busy)
            return;
        if (!this.data.configReady) { this.setData({ error: '请先检查报告服务，再继续填写。' }); return; }
        const validate = (step: number) => this.data.houseEnabled ? validateHouseStep(this.data.input, this.data.house, step) : validateStep(this.data.input, step);
        const error = validate(this.data.step);
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
            const message = validate(step);
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
            const body = this.data.houseEnabled ? houseRequestInput(this.data.input, this.data.house) : this.data.input;
            const result = await reportRequest(this.data.houseEnabled ? '/house' : '', 'POST', body);
            (this as any)._submitted = true;
            wx.removeStorageSync(this.data.houseEnabled ? HOUSE_DRAFT_KEY : DRAFT_KEY);
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

import { reportID, reportRequest } from './api';
import { previewSections, shouldPoll, statusText, canOpenFull, isModernReport, houseConfirmationInputs } from './flow';
import { requireReportLogin } from '../../utils/reportNavigation';
import { requestVirtualPay } from '../../utils/virtualpay';
function paymentBlockHint(reason: string): string {
    if (!reason) return '';
    const messages: { [key: string]: string } = {
        confirmation_required: '请先确认上方需求，再购买完整报告。',
        payment_disabled: '完整报告暂未开放购买，请稍后查看。',
        preview_not_ready: '需求分析尚未完成，请稍后再查看。',
        already_paid: '这份报告已购买，无需重复支付。',
        invalid_confirmation: '需求确认版本已变化，请刷新后重新确认。',
        invalid_preview: '报告信息需要重新核对，请重新填写并生成预览。',
        invalid_frozen_input: '报告信息需要重新核对，请重新填写并生成预览。',
    };
    return messages[reason] || (/[\u4e00-\u9fff]/.test(reason) ? reason : '完整报告暂未开放购买，请稍后重试。');
}
// Both screens use server status; a payment-sheet callback never unlocks content.
export function readerPage(delivery: boolean) {
    const definition: WechatMiniprogram.Page.Options<any, any> = {
        data: {
            id: '', loading: true, error: '', report: null as any, sections: [] as any[], facts: null as any, missing: [] as string[], statusLabel: '', pdfStatusLabel: '正在核对', deliveryHint: '读取报告状态后显示制作与领取进度。', price: '', paying: false, qrURL: '', canDownload: false, documentURL: '', documentStatus: 'loading', documentMode: delivery ? 'full' : 'preview', showDocument: false,
            modern: false, houseReport: false, canRetry: false, contactSupport: false, needsConfirmation: false, confirmationChoice: '', confirmationOptions: [] as any[], confirming: false, canPurchase: false, prioritySummary: '', paymentBlockHint: '', houseInputs: [] as any[]
        },
        onLoad(options: any) {
            try {
                this.setData({ id: reportID(options.id) });
            }
            catch (error: any) {
                this.setData({ error: error.message, loading: false });
            }
        },
        onShow() {
            (this as any)._visible = true;
            (this as any)._polls = 0;
            (this as any)._readFailures = 0;
            this.setData({ confirming: false });
            if (this.data.id && requireReportLogin('/pages/report/' + (delivery ? 'delivery' : 'preview') + '?id=' + this.data.id))
                this.refresh();
        },
        onHide() { this.stop(); },
        onUnload() { this.stop(); },
        stop() {
            (this as any)._visible = false;
            (this as any)._sequence = ((this as any)._sequence || 0) + 1;
            clearTimeout((this as any)._timer);
            (this as any)._confirmation = null;
        },
        async refresh() {
            if (!this.data.id || !(this as any)._visible)
                return;
            if (!requireReportLogin('/pages/report/' + (delivery ? 'delivery' : 'preview') + '?id=' + this.data.id))
                return;
            clearTimeout((this as any)._timer);
            const sequence = ((this as any)._sequence || 0) + 1;
            (this as any)._sequence = sequence;
            try {
                const report = await reportRequest('/' + this.data.id);
                if (!(this as any)._visible || sequence !== (this as any)._sequence)
                    return;
                (this as any)._readFailures = 0;
                const sections = report.preview_snapshot ? previewSections(report.preview_snapshot) : [];
                const modern = isModernReport(report);
                const contactSupport = report.next_action === 'contact_support' || report.error_code === 'image_submission_uncertain';
                const needsConfirmation = modern && !report.confirmed && report.status === 'preview_ready';
                const needsOption = report.protocol_version === 'report-decision-v5' && report.preview_snapshot?.diagnosis?.recipe === 'space';
                const confirmationOptions = needsOption
                    ? [{ value: 'option_a', label: '更倾向方向一' }, { value: 'option_b', label: '更倾向方向二' }, { value: 'undecided', label: '保留两种方向，继续比较' }]
                    : [{ value: 'confirm', label: '确认这些需求与优先顺序' }, { value: 'undecided', label: '部分取舍未定，保留比较' }];
                let pdfStatusLabel = '付款确认后开始';
                let deliveryHint = '付款确认后才开始制作完整报告。如已付款，请刷新核对，无需再次购买。';
                if (report.paid) {
                    pdfStatusLabel = report.status === 'ready' ? '已准备好' : report.status === 'failed' ? '生成未完成' : '制作中';
                    deliveryHint = report.status === 'ready'
                        ? '文件已保存在私有云存储中，请使用本人账号完成领取身份校验。'
                        : report.status === 'failed'
                            ? '本次制作未完成，您可以在下方重试，无需重复付费。反复失败时请联系顾问并提供报告编号。'
                            : '预计约 3–5 分钟，实际以生成进度为准。离开页面不会中断制作，可稍后返回查看。';
                }
                if (modern && report.paid) {
                    if (report.status === 'ready') deliveryHint = '完整报告已保存在私有云存储，使用当前购买账号即可领取；企业微信可用于后续沟通。';
                    else if (report.status !== 'failed') deliveryHint = '正在按您家的需求分阶段生成文字与图片，完成后自动合成 PDF。离开页面不会中断，可稍后回来查看。';
                }
                if (contactSupport) {
                    pdfStatusLabel = '需要核对生成结果';
                    deliveryHint = '部分绘图请求结果需要核对。请联系顾问并提供报告编号，核对完成前无需重新生成或重复购买。';
                }
                this.setData({
                    report, sections, modern, houseReport: report.protocol_version === 'report-house-v1', contactSupport, needsConfirmation, confirmationOptions,
                    canRetry: report.status === 'failed' && !contactSupport && (!modern || report.next_action === 'retry'),
                    paymentBlockHint: paymentBlockHint(report.payment_block_reason || report.quote?.block_reason || ''),
                    canPurchase: report.can_pay === true && (!modern || (report.confirmed === true && report.quote?.can_pay === true)),
                    prioritySummary: report.preview_snapshot?.facts?.['intent.priorities'] || '',
                    houseInputs: report.protocol_version === 'report-house-v1' ? houseConfirmationInputs(report.preview_snapshot?.facts || {}) : [],
                    facts: report.preview_snapshot?.facts || null, missing: report.preview_snapshot?.facts?.missing || [], error: '', loading: false, statusLabel: contactSupport ? '报告生成结果正在核对' : statusText(report.status), pdfStatusLabel, deliveryHint, price: report.quote?.price_fen > 0 ? (report.quote.price_fen / 100).toFixed(2) : '', canDownload: canOpenFull(report)
                });
                const waiting = shouldPoll(report.status) || (delivery && !report.paid);
                const polls = ++(this as any)._polls;
                if (!contactSupport && waiting && (modern || polls < 60))
                    (this as any)._timer = setTimeout(() => this.refresh(), modern && polls > 12 ? 15000 : 5000);
            }
            catch (error: any) {
                if ((this as any)._visible && sequence === (this as any)._sequence) {
                    this.setData({ error: error.message, loading: false });
                    const failures = ((this as any)._readFailures || 0) + 1;
                    (this as any)._readFailures = failures;
                    const previous = this.data.report;
                    const waiting = previous ? shouldPoll(previous.status) || (delivery && !previous.paid) : failures <= 3;
                    if (waiting && ![401, 403, 404].includes(error.statusCode))
                        (this as any)._timer = setTimeout(() => this.refresh(), 15000);
                }
            }
        },
        async retry() {
            if (this.data.paying || this.data.report?.next_action === 'contact_support' || this.data.report?.error_code === 'image_submission_uncertain' || (isModernReport(this.data.report) && this.data.report?.next_action !== 'retry'))
                return;
            this.setData({ paying: true, error: '' });
            try {
                await reportRequest('/' + this.data.id + '/retry', 'POST');
                (this as any)._polls = 0;
                await this.refresh();
            }
            catch (error: any) {
                this.setData({ error: error.message });
            }
            finally {
                this.setData({ paying: false });
            }
        },
        async pay() {
            if (this.data.paying || !this.data.report?.can_pay)
                return;
            const modern = isModernReport(this.data.report);
            if (modern && (!this.data.report.confirmed || this.data.report.quote?.can_pay !== true)) return;
            const scope = this.data.report.protocol_version === 'report-house-v1'
                ? '包含整屋概念方案、分层空间与图示、方案比较、预算边界、设计任务书及行动清单。页数随楼层和方案内容变化。'
                : modern ? '包含需求分析、方向比较、预算口径、设计需求及行动清单。' : '包含三种概念方案及比较、预算建议，共 14 页 PDF。';
            const receipt = modern ? '支付后异步制作，当前购买账号可直接领取，企业微信沟通可选。' : '支付后开始制作，需添加企业微信领取本人专属入口。';
            const agreed = await new Promise<boolean>(resolve => wx.showModal({ title: '购买完整建房报告', content: '价格 ¥' + this.data.price + '。' + scope + receipt + '不含施工图、审批或人工定制设计。', confirmText: '确认购买', success: r => resolve(r.confirm), fail: () => resolve(false) }));
            if (!agreed)
                return;
            this.setData({ paying: true, error: '' });
            let paymentError = '';
            try {
                const result = await requestVirtualPay({ planType: 'report', reportId: this.data.id });
                if (!result.success) {
                    paymentError = result.errMsg || '支付未完成，可重试。';
                    return;
                }
                wx.navigateTo({ url: '/pages/report/delivery?id=' + this.data.id, fail: () => this.setData({ error: '支付结果正在核对，请刷新查看交付状态。' }) });
            }
            catch (error: any) {
                paymentError = error.message || '支付连接中断，请刷新核对后再试。';
            }
            finally {
                this.setData({ paying: false });
                await this.refresh();
                if (paymentError && (this as any)._visible)
                    this.setData({ error: paymentError });
            }
        },
        chooseConfirmation(e: any) {
            const choice = e.currentTarget.dataset.choice;
            if (!this.data.confirmationOptions.some((option: any) => option.value === choice)) return;
            this.setData({ confirmationChoice: choice, error: '' });
        },
        async confirm() {
            if (this.data.confirming || !this.data.needsConfirmation) return;
            if (!this.data.confirmationOptions.some((option: any) => option.value === this.data.confirmationChoice)) { this.setData({ error: '请先确认您的需求或保留待比较的取舍。' }); return; }
            const report = this.data.report, diagnosis = report?.preview_snapshot?.diagnosis;
            if (!diagnosis?.id || !report.input_hash) { this.setData({ error: '确认依据不完整，请刷新报告。' }); return; }
            const sequence = (this as any)._sequence;
            const confirmation = {};
            (this as any)._confirmation = confirmation;
            this.setData({ confirming: true, error: '' });
            try {
                // Empty priorities preserve the server's frozen ordered list;
                // do not reconstruct structured values from display copy.
                await reportRequest('/' + this.data.id + '/confirmation', 'POST', { diagnosis_id: diagnosis.id, input_hash: report.input_hash, choice: this.data.confirmationChoice, priorities: [] });
                if ((this as any)._visible && sequence === (this as any)._sequence) await this.refresh();
            } catch (error: any) {
                if ((this as any)._visible && sequence === (this as any)._sequence) this.setData({ error: error.message });
            } finally {
                if ((this as any)._confirmation === confirmation) {
                    (this as any)._confirmation = null;
                    if ((this as any)._visible) this.setData({ confirming: false });
                }
            }
        },
        delivery() { wx.navigateTo({ url: '/pages/report/delivery?id=' + this.data.id, fail: () => this.setData({ error: '打开失败，请重试。' }) }); },
        preview() { wx.navigateTo({ url: '/pages/report/preview?id=' + this.data.id, fail: () => this.setData({ error: '打开失败，请重试。' }) }); },
        async contact() {
            if (!this.data.report?.paid)
                return;
            this.setData({ error: '' });
            try {
                const contact = await reportRequest('/' + this.data.id + '/wecom', 'POST');
                if (contact.bound === true) {
                    await this.refresh();
                    return;
                }
                if (!/^https:\/\//.test(contact.qr_url || ''))
                    throw new Error('企业微信领取入口暂时不可用，请稍后重试。');
                this.setData({ qrURL: contact.qr_url });
            }
            catch (error: any) {
                this.setData({ error: error.message });
            }
        },
        showQR() {
            if (this.data.qrURL)
                wx.previewImage({ urls: [this.data.qrURL], fail: () => this.setData({ error: '二维码打开失败，请重试。' }) });
        },
        async document() {
            this.setData({ showDocument: true, documentStatus: 'loading', error: '' });
            try {
                const result = await reportRequest('/' + this.data.id + '/document?mode=' + (delivery ? 'full' : 'preview'));
                this.setData({ documentURL: result.document_url, documentStatus: 'ready' });
            }
            catch (error: any) {
                this.setData({ documentStatus: 'failed', error: error.message });
            }
        },
    };
    return definition;
}

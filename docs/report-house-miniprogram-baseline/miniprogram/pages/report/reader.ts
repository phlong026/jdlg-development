import { reportID, reportRequest } from './api';
import { previewSections, shouldPoll, statusText, canOpenFull } from './flow';
import { requireReportLogin } from '../../utils/reportNavigation';
import { requestVirtualPay } from '../../utils/virtualpay';
// Both screens use server status; a payment-sheet callback never unlocks content.
export function readerPage(delivery: boolean) {
    const definition: WechatMiniprogram.Page.Options<any, any> = {
        data: { id: '', loading: true, error: '', report: null as any, sections: [] as any[], facts: null as any, missing: [] as string[], statusLabel: '', pdfStatusLabel: '正在核对', deliveryHint: '读取报告状态后显示制作与领取进度。', price: '', paying: false, qrURL: '', canDownload: false, documentURL: '', documentStatus: 'loading', documentMode: delivery ? 'full' : 'preview', showDocument: false },
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
            if (this.data.id && requireReportLogin('/pages/report/' + (delivery ? 'delivery' : 'preview') + '?id=' + this.data.id))
                this.refresh();
        },
        onHide() { this.stop(); },
        onUnload() { this.stop(); },
        stop() {
            (this as any)._visible = false;
            (this as any)._sequence = ((this as any)._sequence || 0) + 1;
            clearTimeout((this as any)._timer);
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
                const sections = report.preview_snapshot ? previewSections(report.preview_snapshot) : [];
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
                this.setData({ report, sections, facts: report.preview_snapshot?.facts || null, missing: report.preview_snapshot?.facts?.missing || [], error: '', loading: false, statusLabel: statusText(report.status), pdfStatusLabel, deliveryHint, price: report.quote?.price_fen > 0 ? (report.quote.price_fen / 100).toFixed(2) : '', canDownload: canOpenFull(report) });
                const waiting = shouldPoll(report.status) || (delivery && !report.paid);
                if (waiting && ++(this as any)._polls < 60)
                    (this as any)._timer = setTimeout(() => this.refresh(), 5000);
            }
            catch (error: any) {
                if ((this as any)._visible && sequence === (this as any)._sequence)
                    this.setData({ error: error.message, loading: false });
            }
        },
        async retry() {
            if (this.data.paying)
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
            const agreed = await new Promise<boolean>(resolve => wx.showModal({ title: '购买完整建房报告', content: '价格 ¥' + this.data.price + '。包含三种概念方案及比较、预算建议，共 14 页 PDF。支付后开始制作，需添加企业微信领取本人专属入口；不含施工图、审批或人工定制设计。', confirmText: '确认购买', success: r => resolve(r.confirm), fail: () => resolve(false) }));
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

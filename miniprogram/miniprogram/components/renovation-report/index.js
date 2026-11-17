function money(value) { return `¥${Math.round(Number(value || 0)).toLocaleString('zh-CN')}`; }
function wan(value) { return `${(Number(value || 0) / 10000).toFixed(2)}`; }
function breakdown(items, name) { const found = (items || []).find((item) => item.name === name); return Number(found && (found.amount === undefined ? found.value : found.amount) || 0); }
function price(value) { const number = Number(value || 0); return Number(number.toFixed(6)); }

Component({
  properties: {
    report: { type: Object, value: null, observer() { this.refresh(); } },
    readonly: { type: Boolean, value: false },
    detailsExpanded: { type: Boolean, value: false, observer() { this.refresh(); } },
  },
  data: { activeSpace: 0, viewSpaces: [], basicInfo: [], basisText: '', totalWan: '0.00', totalAmountText: '¥0', unitText: '--', engineeringText: '¥0', materialText: '¥0', auxiliaryText: '¥0', laborText: '¥0', ringStyle: '', feeRows: [] },
  lifetimes: { attached() { this.refresh(); } },
  methods: {
    refresh() {
      const report = this.data.report || {}; const renovation = report.renovation || {}; const breakdowns = report.costBreakdown || [];
      const spaces = (renovation.spaces || []).map((space, spaceIndex) => ({
        ...space, amountText: money(space.amount),
        groups: (space.groups || []).map((group, groupIndex) => ({
          ...group, amountText: money(group.amount), expanded: this.data.detailsExpanded && spaceIndex === this.data.activeSpace && groupIndex === 0,
          items: (group.items || []).map((item) => ({ ...item, quantityText: `${item.quantity}${item.unit}`, unitPriceText: price(Number(item.materialUnitPrice || 0) + Number(item.laborAuxUnitPrice || 0)), amountText: money(item.amount) })),
        })),
      }));
      const fees = ['管理费', '设计费', '税费'].map((name) => ({ name, amount: breakdown(breakdowns, name), amountText: breakdown(breakdowns, name) ? money(breakdown(breakdowns, name)) : '未计入' }));
      const materialAmount = breakdown(breakdowns, '主材费'); const auxiliaryAmount = breakdown(breakdowns, '辅材费'); const laborAmount = breakdown(breakdowns, '人工费');
      const engineeringAmount = materialAmount + auxiliaryAmount + laborAmount;
      const materialEnd = engineeringAmount ? (materialAmount / engineeringAmount * 100).toFixed(2) : '0';
      const auxiliaryEnd = engineeringAmount ? ((materialAmount + auxiliaryAmount) / engineeringAmount * 100).toFixed(2) : '0';
      const totalAmount = Number.isFinite(renovation.totalAmount) ? renovation.totalAmount : 0;
      const basisText = { estimated: '按房屋参数估算', measured: '按空间尺寸计算', mixed: '按空间尺寸计算 · 含估算项' }[renovation.quantityBasis]
        || (renovation.pricingVersion === 'renovation-reference-v1' ? '历史参考工程量' : '');
      this.setData({
        basisText,
        viewSpaces: spaces, basicInfo: Array.isArray(report.basicInfo) ? report.basicInfo : [], activeSpace: Math.min(this.data.activeSpace, Math.max(0, spaces.length - 1)),
        totalWan: wan(totalAmount), totalAmountText: money(totalAmount),
        unitText: report.estimatedUnitCost || `${renovation.unitAmount || '--'} 元/㎡`, engineeringText: money(renovation.engineeringAmount || engineeringAmount),
        materialText: money(renovation.materialAmount || breakdown(breakdowns, '主材费')),
        auxiliaryText: money(renovation.auxiliaryAmount || breakdown(breakdowns, '辅材费')),
        laborText: money(renovation.laborAmount || breakdown(breakdowns, '人工费')), ringStyle: `background:conic-gradient(#d7a849 0 ${materialEnd}%,#9ab786 ${materialEnd}% ${auxiliaryEnd}%,#76aee0 ${auxiliaryEnd}% 100%)`, feeRows: fees,
      });
    },
    selectSpace(e) { this.setData({ activeSpace: Number(e.currentTarget.dataset.index) }); },
    toggleGroup(e) {
      const viewSpaces = this.data.viewSpaces.slice(); const space = viewSpaces[this.data.activeSpace]; const groupIndex = Number(e.currentTarget.dataset.index);
      space.groups[groupIndex] = { ...space.groups[groupIndex], expanded: !space.groups[groupIndex].expanded }; this.setData({ viewSpaces });
    },
    onItemTap(e) { if (this.data.readonly) return; this.triggerEvent('itemedit', { item: e.currentTarget.dataset.item }); },
    onFeesTap() { if (this.data.readonly) return; this.triggerEvent('feesedit'); },
    onParametersTap() { if (this.data.readonly) return; this.triggerEvent('parametersedit'); },
  },
});

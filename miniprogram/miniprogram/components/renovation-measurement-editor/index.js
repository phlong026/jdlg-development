const { createMeasurementDraft, resolveRenovationGeometry } = require('../../utils/renovation-geometry');

const numericFields = ['length', 'width', 'height', 'doorWidth', 'doorArea', 'windowWidth', 'windowArea', 'curtainBoxLength', 'tvBackgroundArea', 'cabinetLength'];

function clone(value) { return JSON.parse(JSON.stringify(value)); }
function optionalNumber(value) {
  if (value === null || value === undefined || String(value).trim() === '') return null;
  const number = Number(value);
  return Number.isFinite(number) ? number : NaN;
}
function normalizeMeasurement(measurement) {
  const result = clone(measurement);
  result.floors.forEach((floor) => {
    floor.height = optionalNumber(floor.height);
    floor.spaces.forEach((space) => numericFields.forEach((field) => { space[field] = optionalNumber(space[field]); }));
  });
  return result;
}

Component({
  properties: {
    visible: { type: Boolean, value: false, observer(visible) { if (visible) this.resetDraft(); } },
    input: { type: Object, value: null, observer(input) { if (this.data.visible && input) this.resetDraft(); } },
  },
  data: {
    draft: null,
    activeFloor: 0,
    activeSpace: 0,
    advancedVisible: false,
    areaPreview: '--',
  },
  methods: {
    resetDraft() {
      if (!this.data.input) return;
      try {
        const draft = createMeasurementDraft(clone(this.data.input), this.data.input.measurement || null);
        this.setData({ draft, activeFloor: 0, activeSpace: 0, advancedVisible: false });
        this.refreshPreview(draft, 0, 0);
      } catch (error) {
        console.error('初始化空间尺寸失败:', error);
        wx.showToast({ title: '空间尺寸暂时无法打开', icon: 'none' });
      }
    },
    stopPropagation() {},
    cancel() { this.setData({ draft: null, advancedVisible: false }); this.triggerEvent('cancel'); },
    onModeTap(e) {
      const draft = clone(this.data.draft); draft.mode = e.currentTarget.dataset.mode;
      this.setData({ draft }); this.refreshPreview(draft, this.data.activeFloor, this.data.activeSpace);
    },
    selectFloor(e) {
      const activeFloor = Number(e.currentTarget.dataset.index);
      this.setData({ activeFloor, activeSpace: 0, advancedVisible: false });
      this.refreshPreview(this.data.draft, activeFloor, 0);
    },
    selectSpace(e) {
      const activeSpace = Number(e.currentTarget.dataset.index);
      this.setData({ activeSpace, advancedVisible: false });
      this.refreshPreview(this.data.draft, this.data.activeFloor, activeSpace);
    },
    onSpaceInput(e) {
      const draft = clone(this.data.draft);
      const space = draft.floors[this.data.activeFloor].spaces[this.data.activeSpace];
      space[e.currentTarget.dataset.field] = e.detail.value;
      this.setData({ draft }); this.refreshPreview(draft, this.data.activeFloor, this.data.activeSpace);
    },
    onFloorHeightInput(e) {
      const draft = clone(this.data.draft); draft.floors[this.data.activeFloor].height = e.detail.value;
      this.setData({ draft });
    },
    toggleAdvanced() { this.setData({ advancedVisible: !this.data.advancedVisible }); },
    refreshPreview(draft, floorIndex, spaceIndex) {
      const floor = draft && draft.floors && draft.floors[floorIndex]; const space = floor && floor.spaces && floor.spaces[spaceIndex];
      const length = optionalNumber(space && space.length); const width = optionalNumber(space && space.width);
      const areaPreview = Number.isFinite(length) && length >= 0 && Number.isFinite(width) && width >= 0 ? `${Number((length * width).toFixed(2))}㎡` : '--';
      this.setData({ areaPreview });
    },
    confirm() {
      if (!this.data.draft || !this.data.input) return;
      try {
        const measurement = normalizeMeasurement(this.data.draft);
        const resolved = resolveRenovationGeometry({ ...clone(this.data.input), measurement });
        this.triggerEvent('confirm', { input: resolved.input, summary: resolved.summary });
      } catch (error) {
        console.error('保存空间尺寸失败:', error);
        wx.showToast({ title: String(error && error.message || '请检查空间尺寸'), icon: 'none' });
      }
    },
  },
});

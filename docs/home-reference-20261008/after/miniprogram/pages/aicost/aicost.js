"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/aicost/aicost.ts
const costEstimateShareImage_1 = require("../../utils/costEstimateShareImage");
const costRegions_1 = require("../../utils/costRegions");
const RESULT_STORAGE_KEY = 'aicostEstimateResult';
function getCityOptions(province) {
    return costRegions_1.cityOptionsMap[province] || costRegions_1.cityOptionsMap.福建;
}
const provinceOptions = Object.keys(costRegions_1.cityOptionsMap);
const defaultCityOptions = getCityOptions('福建');
function toPositiveNumber(value) {
    const num = Number(String(value || '').trim());
    return Number.isFinite(num) && num > 0 ? num : 0;
}
function toPositiveInteger(value) {
    const num = Number(String(value || '').trim());
    return Number.isInteger(num) && num > 0 && num <= 20 ? num : 0;
}
function formatWan(amount) {
    return `${(amount / 10000).toFixed(2)}万元`;
}
// 已有成本单价直接使用；新表未覆盖的形式只借用旧规则的相对比例。
const foundationPrices = {
    '独立基础': 650, '条形基础': 900, '筏板基础': 1200,
};
const basementPrices = { '无地下室': 0, '半地下室': 850, '全地下室': 1700 };
const structurePrices = {
    '砖混结构': 685, '框架结构': 735, '钢结构': 830,
};
const roofPrices = {
    '平屋顶': 105, '组合屋顶': 145, '坡屋顶': 185, '燕尾脊': 185 * (1.15 / 1.05),
};
const facadePrices = {
    '涂料外墙': 430, '真石漆': 430, '陶瓷砖外墙': 450,
    '石材外墙': 750, '铝板/金属外立面': 750,
};
const windowPrices = { '经济型': 530, '标准型': 680, '品质型': 1000 };
function estimateTotalCost(input) {
    const { footprintArea, buildingArea, foundationType, structureType, roofType, facadeStyle, windowStandard, basementType } = input;
    const estimable = foundationType !== '桩基础';
    const breakdown = [
        { name: '地基工程', amount: estimable ? Math.round(footprintArea * foundationPrices[foundationType]) : 0,
            note: '土方、垫层、钢筋砼基础、地梁、地下挡墙', estimable },
        { name: '主体工程', amount: Math.round(buildingArea * structurePrices[structureType]),
            note: '梁板柱、砌体、楼梯、钢筋模板（含脚手架等施工措施）', estimable: true },
        { name: '屋顶工程', amount: Math.round(buildingArea * roofPrices[roofType]),
            note: '防水、保温、找平、瓦/砖面层', estimable: true },
        { name: '门窗工程', amount: Math.round(buildingArea * 0.25 * windowPrices[windowStandard]),
            note: '门窗面积按总建筑面积的25%估算，具体做法以设计方案为准', estimable: true },
        { name: '外墙装饰', amount: Math.round(buildingArea * facadePrices[facadeStyle]),
            note: '粉刷、真石漆/瓷砖/石材饰面、线条', estimable: true },
        { name: '水电及其它费用', amount: Math.round(buildingArea * 100),
            note: '水电管线预埋、控制箱、雨水管', estimable: true },
    ];
    if (basementPrices[basementType] > 0) {
        breakdown.push({ name: '地下室工程', amount: Math.round(footprintArea * basementPrices[basementType]),
            note: '不同地下室因方案不同，价格不同。', estimable: true });
    }
    // 分项与总价统一按九折展示，保留到分，避免明细和总价使用不同口径。
    const discountedBreakdown = breakdown.map(item => ({ ...item, amount: Math.round(item.amount * 90) / 100 }));
    const totalCost = Math.round(discountedBreakdown.reduce((sum, item) => sum + item.amount, 0) * 100) / 100;
    return {
        totalCost,
        unitCost: (totalCost / buildingArea).toFixed(2),
        totalLabel: estimable ? '参考总造价' : '已测算部分合计',
        unitLabel: estimable ? '参考单价' : '已测算部分单价',
        breakdown: discountedBreakdown.map(item => ({ ...item,
            amountText: item.estimable ? formatWan(item.amount) : '按图纸核算',
            percent: totalCost > 0 ? Math.round(item.amount / totalCost * 100) : 0,
        })),
        dynamicNotes: ['不同地基因方案不同，价格不同。', '不同屋顶因做法不同，价格不同。'],
    };
}
Page({
    data: {
        province: '福建',
        provinceIndex: 0,
        city: '泉州',
        cityIndex: 0,
        regionIndex: [0, 0],
        footprintArea: '',
        buildingArea: '',
        floorCount: '3',
        firstFloorHeight: '3.6',
        upperFloorHeight: '3.3',
        hasUpperFloorHeight: true,
        buildingStyle: '新闽派',
        buildingStyleIndex: 0,
        structureType: '框架结构',
        structureIndex: 1,
        roofType: '坡屋顶',
        roofIndex: 1,
        basementType: '无地下室',
        basementIndex: 0,
        decorationStyle: '标准型',
        decorationIndex: 1,
        facadeStyle: '真石漆',
        facadeIndex: 1,
        foundationType: '独立基础',
        foundationIndex: 0,
        windowStandard: '标准型',
        windowIndex: 1,
        windowOptions: ['经济型', '标准型', '品质型'],
        buildingStyleOptions: ['新闽派', '现代风格', '新中式', '欧式'],
        structureOptions: ['砖混结构', '框架结构', '钢结构'],
        provinceOptions,
        cityOptions: defaultCityOptions,
        regionOptions: [provinceOptions, defaultCityOptions],
        cityOptionsMap: costRegions_1.cityOptionsMap,
        roofOptions: ['平屋顶', '坡屋顶', '组合屋顶', '燕尾脊'],
        basementOptions: ['无地下室', '半地下室', '全地下室'],
        decorationOptions: ['经济型', '标准型', '品质型'],
        facadeOptions: ['涂料外墙', '真石漆', '陶瓷砖外墙', '石材外墙', '铝板/金属外立面'],
        foundationOptions: ['独立基础', '条形基础', '筏板基础', '桩基础'],
    },
    onLoad(options = {}) {
        const updates = {};
        const provinceIndex = provinceOptions.indexOf(options.province || '');
        const cityOptions = provinceIndex >= 0 ? getCityOptions(provinceOptions[provinceIndex]) : [];
        const cityIndex = cityOptions.indexOf(options.city || '');
        if (provinceIndex >= 0 && cityIndex >= 0) {
            const province = provinceOptions[provinceIndex];
            updates.province = province;
            updates.provinceIndex = provinceIndex;
            updates.city = cityOptions[cityIndex];
            updates.cityIndex = cityIndex;
            updates.cityOptions = cityOptions;
            updates.regionOptions = [provinceOptions, cityOptions];
            updates.regionIndex = [provinceIndex, cityIndex];
        }
        const buildingArea = String(options.buildingArea || '').trim();
        if (toPositiveNumber(buildingArea)) {
            updates.buildingArea = buildingArea;
        }
        if (Object.keys(updates).length > 0) {
            this.setData(updates);
        }
    },
    onShareAppMessage() {
        const title = '10秒估算建房报价 平台大数据，造价更精准';
        const path = '/pages/aicost/aicost';
        return {
            title,
            path,
            // The form has no estimate yet; never reuse a previous report's price.
            promise: (0, costEstimateShareImage_1.prepareCostEstimateShareImage)({
                city: '',
                buildingArea: '',
                structureType: '',
                windowStandard: '',
            }).then((imageUrl) => imageUrl ? { title, path, imageUrl } : { title, path }),
        };
    },
    onFootprintAreaInput(e) {
        this.setData({ footprintArea: e.detail.value });
    },
    onBuildingAreaInput(e) {
        this.setData({ buildingArea: e.detail.value });
    },
    onFloorCountInput(e) {
        const floorCount = String(e.detail.value || '');
        const count = toPositiveInteger(floorCount);
        this.setData({
            floorCount,
            hasUpperFloorHeight: count > 1,
        });
    },
    onFirstFloorHeightInput(e) {
        this.setData({ firstFloorHeight: e.detail.value });
    },
    onUpperFloorHeightInput(e) {
        this.setData({ upperFloorHeight: e.detail.value });
    },
    onRegionColumnChange(e) {
        const options = this.data.provinceOptions;
        const column = Number(e.detail.column);
        const value = Number(e.detail.value);
        const regionIndex = [...this.data.regionIndex];
        regionIndex[column] = value;
        if (column !== 0) {
            this.setData({ regionIndex });
            return;
        }
        const province = options[value];
        if (!province) {
            return;
        }
        const cityOptions = getCityOptions(province);
        this.setData({
            cityOptions,
            regionOptions: [options, cityOptions],
            regionIndex: [value, 0],
        });
    },
    onRegionChange(e) {
        const options = this.data.provinceOptions;
        const regionIndex = (e.detail.value || [0, 0]);
        const provinceIndex = Number(regionIndex[0] || 0);
        const cityIndex = Number(regionIndex[1] || 0);
        const province = options[provinceIndex];
        if (!province) {
            return;
        }
        const cityOptions = getCityOptions(province);
        const city = cityOptions[cityIndex] || cityOptions[0];
        if (!city) {
            return;
        }
        this.setData({
            province,
            provinceIndex,
            city,
            cityIndex,
            cityOptions,
            regionOptions: [options, cityOptions],
            regionIndex: [provinceIndex, cityIndex],
        });
    },
    onRegionCancel() {
        const cityOptions = getCityOptions(this.data.province);
        this.setData({
            cityOptions,
            regionOptions: [this.data.provinceOptions, cityOptions],
            regionIndex: [this.data.provinceIndex, this.data.cityIndex],
        });
    },
    onOptionChange(e) {
        const field = String(e.currentTarget.dataset.field || '');
        const optionsKey = String(e.currentTarget.dataset.options || '');
        const indexField = String(e.currentTarget.dataset.indexField || '');
        const options = (this.data[optionsKey] || []);
        const selectedIndex = Number(e.detail.value);
        const selectedValue = options[selectedIndex];
        if (!field || !indexField || !selectedValue) {
            return;
        }
        this.setData({
            [field]: selectedValue,
            [indexField]: selectedIndex,
        });
    },
    onEstimateCost() {
        const data = this.data;
        const footprintArea = toPositiveNumber(data.footprintArea);
        const buildingArea = toPositiveNumber(data.buildingArea);
        const floorCount = toPositiveInteger(data.floorCount);
        const firstFloorHeight = toPositiveNumber(data.firstFloorHeight);
        const upperFloorHeight = toPositiveNumber(data.upperFloorHeight);
        let error = '';
        if (!data.province || !data.city)
            error = '请选择所在地区';
        else if (!footprintArea)
            error = '请填写建筑占地面积';
        else if (!buildingArea)
            error = '请填写总建筑面积';
        else if (buildingArea < footprintArea)
            error = '总建筑面积不能小于占地面积';
        else if (!floorCount)
            error = '楼层数请填写1至20的整数';
        else if (!firstFloorHeight)
            error = '请填写首层高度';
        else if (floorCount > 1 && !upperFloorHeight)
            error = '请填写其他楼层高度';
        else if ((data.foundationType !== '桩基础' && !foundationPrices[data.foundationType]) ||
            !structurePrices[data.structureType] || !roofPrices[data.roofType] ||
            !facadePrices[data.facadeStyle] || !windowPrices[data.windowStandard] ||
            basementPrices[data.basementType] === undefined)
            error = '请重新选择建筑参数';
        if (error) {
            wx.showToast({ title: error, icon: 'none' });
            return;
        }
        const result = estimateTotalCost({
            footprintArea, buildingArea, foundationType: data.foundationType, basementType: data.basementType,
            structureType: data.structureType, roofType: data.roofType,
            facadeStyle: data.facadeStyle, windowStandard: data.windowStandard,
        });
        // 保存本次提交的信息快照，避免再次编辑表单后报告内容与金额不一致。
        const basicInfo = [
            { label: '所在地区', value: `${data.province} / ${data.city}`, fullWidth: true },
            { label: '建筑占地面积', value: `${footprintArea}㎡` },
            { label: '总建筑面积', value: `${buildingArea}㎡` },
            { label: '楼层数', value: `${floorCount}层` },
            { label: '首层高度', value: `${firstFloorHeight}m` },
            { label: '其他楼层高度', value: floorCount > 1 ? `${upperFloorHeight}m` : '不适用' },
            { label: '地下室形式', value: data.basementType },
            { label: '建筑风格', value: data.buildingStyle },
            { label: '结构形式', value: data.structureType },
            { label: '屋顶形式', value: data.roofType },
            { label: '外立面样式', value: data.facadeStyle },
            { label: '建筑基础', value: data.foundationType },
            { label: '装修标准', value: data.decorationStyle },
            { label: '门窗标准', value: data.windowStandard },
        ];
        try {
            wx.setStorageSync(RESULT_STORAGE_KEY, {
                version: 4,
                estimatedTotalCost: formatWan(result.totalCost),
                estimatedUnitCost: `${result.unitCost}元/㎡`,
                totalLabel: result.totalLabel,
                unitLabel: result.unitLabel,
                basicInfo,
                costBreakdown: result.breakdown,
                dynamicNotes: result.dynamicNotes,
            });
        }
        catch (error) {
            console.error('保存造价测算报告失败:', error);
            wx.showToast({ title: '报告保存失败，请重试', icon: 'none' });
            return;
        }
        wx.navigateTo({
            url: '/pages/aicostresult/aicostresult',
            fail: () => {
                wx.showToast({ title: '测算报告打开失败，请重试', icon: 'none' });
            },
        });
    },
});

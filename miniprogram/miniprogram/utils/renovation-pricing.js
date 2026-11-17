const PRICING_VERSION = 'renovation-reference-v1';
const STANDARD_MULTIPLIERS = { economy: 0.7, standard: 1, quality: 1.2 };
const HOUSE_MULTIPLIERS = { new: 1, old: 1.1087 };
const ROOM_IDS = ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony'];
const AUXILIARY_REFERENCE = 12058;
const LABOR_REFERENCE = 28952;
const MAX_QUANTITY = 1000000;

function createDefaultRenovationInput() {
  return {
    province: '福建',
    city: '泉州',
    area: 100,
    width: 10,
    depth: 10,
    floorCount: 1,
    height: 3,
    houseType: 'new',
    standard: 'standard',
    style: '现代简约',
    rooms: { bedroom: 3, living: 2, kitchen: 1, bathroom: 2, balcony: 1 },
  };
}

function invalid(name) {
  throw new RangeError(`装修报价参数不正确：${name}`);
}

function finiteNumber(value, name, min, max) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < min || value > max) invalid(name);
  return value;
}

function integer(value, name, min, max) {
  finiteNumber(value, name, min, max);
  if (!Number.isInteger(value)) invalid(name);
  return value;
}

function optionalRecordedNumber(value, name, min, max) {
  if (value === null || value === undefined || value === '') return null;
  return finiteNumber(value, name, min, max);
}

function cloneValue(value) {
  if (Array.isArray(value)) return value.map(cloneValue);
  if (value && typeof value === 'object') {
    const output = {};
    for (const key of Object.keys(value)) output[key] = cloneValue(value[key]);
    return output;
  }
  return value;
}

function cloneFloorValue(value) {
  if (typeof value === 'number' && !Number.isFinite(value)) invalid('楼层信息');
  if (Array.isArray(value)) return value.map(cloneFloorValue);
  if (value && typeof value === 'object') {
    const output = {};
    for (const key of Object.keys(value)) output[key] = cloneFloorValue(value[key]);
    return output;
  }
  if (!['string', 'number', 'boolean', 'undefined'].includes(typeof value) && value !== null) invalid('楼层信息');
  return value;
}

function normalizeInput(input) {
  if (!input || typeof input !== 'object' || Array.isArray(input)) invalid('输入数据');
  const rooms = input.rooms;
  if (!rooms || typeof rooms !== 'object' || Array.isArray(rooms)) invalid('房间数量');
  const normalizedRooms = {};
  const roomNames = { bedroom: '卧室数', living: '客餐厅数', kitchen: '厨房数', bathroom: '卫生间数', balcony: '阳台数' };
  for (const id of ROOM_IDS) normalizedRooms[id] = integer(rooms[id], roomNames[id], 0, 50);
  if (!Object.prototype.hasOwnProperty.call(STANDARD_MULTIPLIERS, input.standard)) invalid('装修档次');
  if (!Object.prototype.hasOwnProperty.call(HOUSE_MULTIPLIERS, input.houseType)) invalid('房屋类型');
  if (typeof input.province !== 'string' || typeof input.city !== 'string' || typeof input.style !== 'string') invalid('地区或风格');
  if (input.floors !== undefined && !Array.isArray(input.floors)) invalid('楼层信息');

  const normalized = {
    province: input.province,
    city: input.city,
    area: finiteNumber(input.area, '面积', 1, 100000),
    width: optionalRecordedNumber(input.width, '面宽', 0.1, 10000),
    depth: optionalRecordedNumber(input.depth, '进深', 0.1, 10000),
    floorCount: integer(input.floorCount, '层数', 1, 20),
    height: input.height === null || input.height === undefined || input.height === ''
      ? 3
      : finiteNumber(input.height, '层高', 0.1, 100),
    houseType: input.houseType,
    standard: input.standard,
    style: input.style,
    rooms: normalizedRooms,
  };
  if (input.floors !== undefined) normalized.floors = cloneFloorValue(input.floors);
  return normalized;
}

const qty = {
  fixed: value => () => value,
  area: factor => input => input.area * factor,
  room: id => input => input.rooms[id],
};

function item(id, name, unit, quantity, materialUnitPrice, laborAuxUnitPrice) {
  return { id, name, unit, quantity, materialUnitPrice, laborAuxUnitPrice };
}

const SPACE_DEFINITIONS = [
  {
    id: 'bedroom', name: '卧室', count: input => input.rooms.bedroom,
    groups: [
      { id: 'ceiling', name: '天花板', items: [
        item('bedroom.ceiling.primer', '顶面墙固', '㎡', qty.fixed(30), 0, 9.6),
        item('bedroom.ceiling.putty', '顶面腻子', '㎡', qty.fixed(30), 0, 28),
        item('bedroom.ceiling.latex', '顶面乳胶漆', '㎡', qty.fixed(30), 0, 17.6),
        item('bedroom.ceiling.gypsum_line', '顶面石膏线', 'm', qty.fixed(22), 0, 28),
        item('bedroom.ceiling.curtain_box', '窗帘盒', 'm', qty.fixed(4), 0, 126),
      ] },
      { id: 'wall', name: '墙面', items: [
        item('bedroom.wall.primer', '墙面墙固', '㎡', qty.fixed(70), 0, 9.6),
        item('bedroom.wall.putty', '墙面腻子', '㎡', qty.fixed(70), 0, 28),
        item('bedroom.wall.mesh', '墙面挂网', '㎡', qty.fixed(70), 0, 9.6),
      ] },
      { id: 'floor', name: '地面', items: [
        item('bedroom.floor.seam', '地砖美缝', '㎡', qty.fixed(30), 0, 24),
        item('bedroom.floor.tile', '地砖铺贴', '㎡', qty.fixed(30), 112, 80),
        item('bedroom.floor.skirting', '踢脚线', 'm', qty.fixed(22), 8, 24),
      ] },
      { id: 'door', name: '室内门', items: [
        item('bedroom.door.bedroom_door', '卧室门', '套', qty.room('bedroom'), 1200, 0),
      ] },
    ],
  },
  {
    id: 'living', name: '客餐厅', count: input => input.rooms.living,
    groups: [
      { id: 'ceiling', name: '天花板', items: [
        item('living.ceiling.primer', '顶面墙固', '㎡', qty.fixed(30), 0, 9.6),
        item('living.ceiling.putty', '顶面腻子', '㎡', qty.fixed(30), 0, 28),
        item('living.ceiling.latex', '顶面乳胶漆', '㎡', qty.fixed(30), 0, 17.6),
        item('living.ceiling.gypsum_line', '顶面石膏线', 'm', qty.fixed(22), 0, 20),
        item('living.ceiling.curtain_box', '窗帘盒', 'm', qty.fixed(4), 0, 126),
      ] },
      { id: 'wall', name: '墙面', items: [
        item('living.wall.primer', '墙面墙固', '㎡', qty.fixed(70), 0, 9.6),
        item('living.wall.putty', '墙面腻子', '㎡', qty.fixed(70), 0, 28),
        item('living.wall.latex', '墙面乳胶漆', '㎡', qty.fixed(70), 0, 17.6),
        item('living.wall.tv_background', '电视背景墙', '㎡', qty.fixed(10), 400, 80),
      ] },
      { id: 'floor', name: '地面', items: [
        item('living.floor.tile', '地砖铺贴', '㎡', qty.fixed(30), 96, 80),
        item('living.floor.seam', '地砖美缝', '㎡', qty.fixed(30), 0, 24),
        item('living.floor.skirting', '踢脚线', 'm', qty.fixed(22), 8, 24),
      ] },
      { id: 'threshold', name: '门槛石', items: [
        item('living.threshold.stone', '客餐厅门槛石', '块', qty.fixed(1), 56, 56),
      ] },
    ],
  },
  {
    id: 'kitchen', name: '厨房', count: input => input.rooms.kitchen,
    groups: [
      { id: 'ceiling', name: '天花板', items: [item('kitchen.ceiling.aluminum', '铝扣板吊顶', '㎡', qty.fixed(6), 112, 16)] },
      { id: 'wall', name: '墙面', items: [
        item('kitchen.wall.tile', '墙砖铺贴', '㎡', qty.fixed(25), 72, 80),
        item('kitchen.wall.pipe_wrap', '厨房包管', '项', qty.room('kitchen'), 0, 400),
      ] },
      { id: 'floor', name: '地面', items: [
        item('kitchen.floor.leak', '地面防漏处理', '㎡', qty.fixed(6), 0, 52),
        item('kitchen.floor.tile', '地砖铺贴', '㎡', qty.fixed(6), 56, 80),
        item('kitchen.floor.threshold', '厨房门槛石', '块', input => input.rooms.kitchen * 2, 40, 56),
      ] },
      { id: 'kitchenware', name: '厨具', items: [
        item('kitchen.utensils.cabinet', '橱柜', 'm', qty.fixed(3), 1600, 0),
        item('kitchen.utensils.sink', '水槽龙头', '套', qty.room('kitchen'), 400, 0),
        item('kitchen.utensils.hardware', '五金安装', '项', qty.area(0.1), 80, 4),
        item('kitchen.utensils.hood_stove', '烟机灶具', '套', qty.room('kitchen'), 2400, 0),
      ] },
      { id: 'door', name: '室内门', items: [
        item('kitchen.door.threshold', '厨房门槛石', '块', input => input.rooms.kitchen * 2, 56, 56),
        item('kitchen.door.sliding', '厨房推拉门', '套', qty.room('kitchen'), 1440, 0),
      ] },
      { id: 'lighting', name: '灯具', items: [item('kitchen.light.main', '厨房灯', '套', qty.room('kitchen'), 80, 40)] },
    ],
  },
  {
    id: 'bathroom', name: '卫生间', count: input => input.rooms.bathroom,
    groups: [
      { id: 'ceiling', name: '天花板', items: [item('bathroom.ceiling.aluminum', '铝扣板吊顶', '㎡', qty.fixed(8), 112, 16)] },
      { id: 'wall', name: '墙面', items: [
        item('bathroom.wall.waterproof', '墙面防水', '㎡', qty.fixed(30), 0, 52),
        item('bathroom.wall.pipe', '上下水管', '项', qty.room('bathroom'), 56, 0),
        item('bathroom.wall.vertical_pipe', '立管包管', '项', qty.room('bathroom'), 0, 280),
        item('bathroom.wall.tile', '墙砖铺贴', '㎡', qty.fixed(30), 64, 80),
      ] },
      { id: 'floor', name: '地面', items: [
        item('bathroom.floor.tile', '地砖铺贴', '㎡', qty.fixed(8), 56, 80),
        item('bathroom.floor.sunken', '下沉式沉箱处理', '㎡', qty.fixed(8), 0, 224),
        item('bathroom.floor.threshold', '卫生间门槛石', '块', qty.room('bathroom'), 40, 56),
      ] },
      { id: 'sanitary', name: '卫浴用具', items: [
        item('bathroom.fixtures.toilet', '马桶', '套', qty.room('bathroom'), 800, 160),
        item('bathroom.fixtures.basin', '洗手盆', '套', qty.room('bathroom'), 1280, 40),
        item('bathroom.fixtures.shower', '花洒', '套', qty.room('bathroom'), 680, 0),
        item('bathroom.fixtures.heater', '风暖浴霸', '个', qty.room('bathroom'), 800, 0),
      ] },
      { id: 'door', name: '室内门', items: [
        item('bathroom.door.threshold', '卫生间门槛石', '块', qty.room('bathroom'), 56, 56),
        item('bathroom.door.aluminum', '铝合金门', '套', qty.room('bathroom'), 880, 0),
      ] },
    ],
  },
  {
    id: 'balcony', name: '阳台', count: input => input.rooms.balcony,
    groups: [
      { id: 'ceiling', name: '天花板', items: [
        item('balcony.ceiling.primer', '顶面墙固', '㎡', qty.fixed(8), 0, 9.6),
        item('balcony.ceiling.putty', '顶面腻子', '㎡', qty.fixed(8), 0, 28),
        item('balcony.ceiling.latex', '顶面乳胶漆', '㎡', qty.fixed(8), 0, 17.6),
      ] },
      { id: 'wall', name: '墙面', items: [item('balcony.wall.vertical_pipe', '阳台立管', '项', qty.room('balcony'), 0, 280)] },
      { id: 'floor', name: '地面', items: [item('balcony.floor.tile', '地砖铺贴', '㎡', qty.fixed(6), 56, 80)] },
    ],
  },
  {
    id: 'common', name: '全屋公共', count: () => 1,
    groups: [
      { id: 'utilities', name: '水电改造', items: [
        item('common.utilities.switch_wiring', '开关插座布线', '位', qty.area(0.6), 38.5, 35.75),
        item('common.utilities.network_tv', '网络电视点位', '位', qty.fixed(2), 77, 71.5),
        item('common.utilities.switch_install', '开关插座安装', '位', qty.area(0.6), 16.5, 5.5),
        item('common.utilities.electric_box', '电箱', '个', qty.fixed(2), 165, 220),
        item('common.utilities.water_pipe', '给水管', 'm', qty.area(0.4), 23.65, 24.75),
        item('common.utilities.channel_repair', '开槽修复', '项', qty.fixed(1), 110, 440),
      ] },
      { id: 'handling', name: '搬运清洁', items: [
        item('common.cleaning.handling', '材料搬运', '㎡', qty.area(1), 0, 8),
        item('common.cleaning.daily', '日常保洁', '㎡', qty.area(1), 0, 6.4),
        item('common.cleaning.garbage', '垃圾清运', '㎡', qty.area(1), 0, 9.6),
      ] },
      { id: 'demolition', name: '拆改与保护', items: [
        item('common.protection.corner', '阴阳角处理', '㎡', qty.area(1), 0, 4.8),
        item('common.protection.finished', '成品保护', '㎡', qty.area(1), 0, 4.48),
      ] },
    ],
  },
];

function roundMoney(value) {
  return Math.round((value + Number.EPSILON) * 100) / 100;
}

function effectiveUnitPrice(value, multiplier) {
  return Math.round(value * multiplier * 1000000) / 1000000;
}

function normalizeSelections(selections, knownIds) {
  const value = selections === undefined ? {} : selections;
  if (!value || typeof value !== 'object' || Array.isArray(value)) invalid('报价选项');
  const excludedItems = value.excludedItems === undefined ? [] : value.excludedItems;
  const quantities = value.quantities === undefined ? {} : value.quantities;
  const fees = value.fees === undefined ? {} : value.fees;
  if (!Array.isArray(excludedItems) || !quantities || typeof quantities !== 'object' || Array.isArray(quantities) || !fees || typeof fees !== 'object' || Array.isArray(fees)) invalid('报价选项');
  for (const id of excludedItems) {
    if (typeof id !== 'string' || !knownIds.has(id)) throw new RangeError(`未知装修报价项目：${String(id)}`);
  }
  for (const id of Object.keys(quantities)) {
    if (!knownIds.has(id)) throw new RangeError(`未知装修报价项目：${id}`);
    finiteNumber(quantities[id], `工程量.${id}`, 0, MAX_QUANTITY);
  }
  for (const id of Object.keys(fees)) {
    if (!['management', 'design', 'tax'].includes(id) || typeof fees[id] !== 'boolean') invalid(`费用选项.${id}`);
  }
  return {
    excludedItems: new Set(excludedItems),
    quantities,
    fees: { management: fees.management !== false, design: fees.design !== false, tax: fees.tax === true },
  };
}

function estimateRenovation(input, selections) {
  const normalizedInput = normalizeInput(input);
  const knownIds = new Set();
  for (const space of SPACE_DEFINITIONS) for (const group of space.groups) for (const definition of group.items) knownIds.add(definition.id);
  const normalizedSelections = normalizeSelections(selections, knownIds);
  const multiplier = STANDARD_MULTIPLIERS[normalizedInput.standard] * HOUSE_MULTIPLIERS[normalizedInput.houseType];

  const calculated = [];
  for (const spaceDefinition of SPACE_DEFINITIONS) {
    const active = spaceDefinition.count(normalizedInput) > 0;
    for (const groupDefinition of spaceDefinition.groups) {
      for (const definition of groupDefinition.items) {
        const defaultQuantity = definition.quantity(normalizedInput);
        const quantity = Object.prototype.hasOwnProperty.call(normalizedSelections.quantities, definition.id)
          ? normalizedSelections.quantities[definition.id]
          : defaultQuantity;
        const included = active && !normalizedSelections.excludedItems.has(definition.id);
        const materialUnitPrice = effectiveUnitPrice(definition.materialUnitPrice, multiplier);
        const laborAuxUnitPrice = effectiveUnitPrice(definition.laborAuxUnitPrice, multiplier);
        const amount = active && included ? Math.round(quantity * (materialUnitPrice + laborAuxUnitPrice)) : 0;
        const materialAmount = active && included ? Math.round(quantity * materialUnitPrice) : 0;
        calculated.push({
          spaceDefinition, groupDefinition, definition, quantity, included,
          materialUnitPrice, laborAuxUnitPrice, amount, materialAmount,
        });
      }
    }
  }

  const spaces = [];
  let cursor = 0;
  for (const spaceDefinition of SPACE_DEFINITIONS) {
    const groups = [];
    for (const groupDefinition of spaceDefinition.groups) {
      const items = groupDefinition.items.map(definition => {
        const entry = calculated[cursor];
        const output = {
          id: definition.id,
          name: definition.name,
          unit: definition.unit,
          quantity: entry.quantity,
          materialUnitPrice: entry.materialUnitPrice,
          laborAuxUnitPrice: entry.laborAuxUnitPrice,
          amount: entry.amount,
          included: entry.included,
        };
        cursor += 1;
        return output;
      });
      groups.push({ id: groupDefinition.id, name: groupDefinition.name, amount: items.reduce((sum, output) => sum + output.amount, 0), items });
    }
    spaces.push({
      id: spaceDefinition.id,
      name: spaceDefinition.name,
      count: spaceDefinition.count(normalizedInput),
      amount: groups.reduce((sum, group) => sum + group.amount, 0),
      groups,
    });
  }

  const engineeringAmount = calculated.reduce((sum, entry) => sum + entry.amount, 0);
  const materialAmount = calculated.reduce((sum, entry) => sum + entry.materialAmount, 0);
  const nonMaterialAmount = engineeringAmount - materialAmount;
  const auxiliaryAmount = Math.round(nonMaterialAmount * AUXILIARY_REFERENCE / (AUXILIARY_REFERENCE + LABOR_REFERENCE));
  const laborAmount = nonMaterialAmount - auxiliaryAmount;
  const feeDefinitions = [
    { id: 'management', name: '管理费', rate: 0.1 },
    { id: 'design', name: '设计费', rate: 0.04 },
    { id: 'tax', name: '税金', rate: 0.034 },
  ];
  const fees = feeDefinitions.map(fee => {
    const included = normalizedSelections.fees[fee.id];
    return { ...fee, amount: included ? Math.round(engineeringAmount * fee.rate) : 0, included };
  });
  const totalAmount = engineeringAmount + fees.reduce((sum, fee) => sum + fee.amount, 0);
  return {
    pricingVersion: PRICING_VERSION,
    input: cloneValue(normalizedInput),
    spaces,
    engineeringAmount,
    materialAmount,
    auxiliaryAmount,
    laborAmount,
    fees,
    totalAmount,
    unitAmount: roundMoney(totalAmount / normalizedInput.area),
  };
}

module.exports = { createDefaultRenovationInput, estimateRenovation };

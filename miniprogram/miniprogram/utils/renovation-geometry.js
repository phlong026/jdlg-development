const SPACE_TYPES = ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony'];
const TYPE_NAMES = { bedroom: '卧室', living: '客餐厅', kitchen: '厨房', bathroom: '卫生间', balcony: '阳台' };
const AREA_WEIGHTS = { bedroom: 12, living: 20, kitchen: 8, bathroom: 5, balcony: 6 };
const TYPE_RATIOS = { bedroom: 1.25, living: 1.5, kitchen: 1.4, bathroom: 1.2, balcony: 2 };
const NULLABLE_FIELDS = ['length', 'width', 'height', 'doorWidth', 'doorArea', 'windowWidth', 'windowArea', 'curtainBoxLength', 'tvBackgroundArea', 'cabinetLength'];

function fail(name) { throw new RangeError(`装修测量参数不正确：${name}`); }
function round6(value) { return Math.round((value + Number.EPSILON) * 1000000) / 1000000; }
function positive(value, name, nullable) {
  if ((value === null || value === undefined || value === '') && nullable) return null;
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) fail(name);
  return value;
}
function nonNegative(value, name, nullable) {
  if ((value === null || value === undefined || value === '') && nullable) return null;
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) fail(name);
  return value;
}
function clone(value) {
  if (Array.isArray(value)) return value.map(clone);
  if (value && typeof value === 'object') {
    const output = {};
    for (const key of Object.keys(value)) output[key] = clone(value[key]);
    return output;
  }
  return value;
}
function emptyRooms() { return { bedroom: 0, living: 0, kitchen: 0, bathroom: 0, balcony: 0 }; }
function normalizeRooms(rooms) {
  const output = emptyRooms();
  if (rooms !== undefined && (!rooms || typeof rooms !== 'object' || Array.isArray(rooms))) fail('房间数量');
  const source = rooms && typeof rooms === 'object' && !Array.isArray(rooms) ? rooms : {};
  for (const type of SPACE_TYPES) {
    const value = source[type] === undefined ? 0 : source[type];
    if (!Number.isInteger(value) || value < 0 || value > 50) fail(`${TYPE_NAMES[type]}数量`);
    output[type] = value;
  }
  return output;
}
function distributeCounts(rooms, floorCount) {
  const output = Array.from({ length: floorCount }, emptyRooms);
  const instances = [];
  for (const type of SPACE_TYPES) for (let index = 0; index < rooms[type]; index += 1) instances.push(type);
  for (let index = 0; index < instances.length; index += 1) output[index % floorCount][instances[index]] += 1;
  return output;
}
function normalizeFloorBlueprints(input, allowMeasuredAreaDrift, allowMissingArea) {
  if (input.floorCount !== undefined && (!Number.isInteger(input.floorCount) || input.floorCount < 1 || input.floorCount > 20)) fail('层数');
  if (input.floors !== undefined && !Array.isArray(input.floors)) fail('楼层信息');
  const floorCount = input.floorCount === undefined ? 1 : input.floorCount;
  const totalArea = allowMissingArea && !(typeof input.area === 'number' && Number.isFinite(input.area) && input.area > 0)
    ? 0 : positive(input.area, '面积', false);
  const supplied = Array.isArray(input.floors) && input.floors.length > 0;
  if (supplied) {
    if (input.floors.length !== floorCount) fail('楼层数量与层数不一致');
    const floors = input.floors.map((floor, offset) => {
      if (!floor || typeof floor !== 'object' || Array.isArray(floor)) fail(`第${offset + 1}层信息`);
      return {
        index: offset,
        area: allowMissingArea && !(typeof floor.area === 'number' && Number.isFinite(floor.area) && floor.area > 0)
          ? 0 : positive(floor.area, `第${offset + 1}层面积`, false),
        height: positive(floor.height, `第${offset + 1}层层高`, true),
        rooms: normalizeRooms(floor.rooms),
      };
    });
    const areaSum = floors.reduce((sum, floor) => sum + floor.area, 0);
    if (!allowMeasuredAreaDrift && Math.abs(areaSum - totalArea) > 0.01) fail('各层面积合计必须等于总面积');
    const roomTotal = floors.reduce((sum, floor) => sum + SPACE_TYPES.reduce((floorSum, type) => floorSum + floor.rooms[type], 0), 0);
    if (!allowMeasuredAreaDrift && roomTotal > 0) {
      const emptyFloor = floors.find(floor => SPACE_TYPES.every(type => floor.rooms[type] === 0));
      if (emptyFloor) fail(`第${emptyFloor.index + 1}层未设置房间`);
    }
    return floors;
  }
  const counts = distributeCounts(normalizeRooms(input.rooms), floorCount);
  let used = 0;
  return counts.map((rooms, offset) => {
    const area = offset === floorCount - 1 ? round6(totalArea - used) : round6(totalArea / floorCount);
    used += area;
    return { index: offset, area, height: null, rooms };
  });
}
function makeEmptySpace(floorIndex, type, instanceIndex, previousById) {
  const id = `${floorIndex}.${type}.${instanceIndex}`;
  const previous = previousById.get(id);
  const space = { id, type, name: `${TYPE_NAMES[type]}${instanceIndex + 1}` };
  for (const field of NULLABLE_FIELDS) space[field] = previous && previous[field] !== undefined ? previous[field] : null;
  if (previous && typeof previous.name === 'string' && previous.name.trim()) space.name = previous.name;
  return space;
}
function createMeasurementDraft(input, previous) {
  if (!input || typeof input !== 'object' || Array.isArray(input)) fail('输入数据');
  const previousMeasurement = previous && previous.measurement ? previous.measurement : previous;
  const previousById = new Map();
  if (previousMeasurement && Array.isArray(previousMeasurement.floors)) {
    for (const floor of previousMeasurement.floors) for (const space of Array.isArray(floor.spaces) ? floor.spaces : []) {
      if (space && typeof space.id === 'string') previousById.set(space.id, space);
    }
  }
  const blueprints = normalizeFloorBlueprints(input, true, true);
  const floors = blueprints.map(blueprint => {
    const spaces = [];
    for (const type of SPACE_TYPES) for (let index = 0; index < blueprint.rooms[type]; index += 1) {
      spaces.push(makeEmptySpace(blueprint.index, type, index, previousById));
    }
    const previousFloor = previousMeasurement && Array.isArray(previousMeasurement.floors)
      ? previousMeasurement.floors.find(floor => Number(floor.index) === blueprint.index) : null;
    return { index: blueprint.index, height: previousFloor && previousFloor.height !== undefined ? previousFloor.height : null, spaces };
  });
  return { version: 1, mode: previousMeasurement && previousMeasurement.mode === 'measured' ? 'measured' : 'estimated', floors };
}

function emptyMetric() {
  return { floorArea: 0, ceilingArea: 0, perimeter: 0, wallArea: 0, skirtingLength: 0, curtainBoxLength: 0, tvBackgroundArea: 0, cabinetLength: 0, count: 0, pipeEquivalent: 0 };
}
function addMetric(target, values) {
  for (const key of Object.keys(target)) target[key] = round6(target[key] + (values[key] || 0));
}
function openingDefaults(type, width, height) {
  const doorWidth = type === 'balcony' ? 1.6 : 0.9;
  const hasWindow = ['bedroom', 'living', 'kitchen', 'bathroom', 'balcony'].includes(type);
  const windowWidth = hasWindow ? Math.min(Math.max(width * 0.4, 0.6), type === 'balcony' ? 2.4 : 1.8) : 0;
  return { doorWidth, doorArea: Math.min(doorWidth * 2.1, 2 * (width + width) * height * 0.2), windowWidth, windowArea: Math.min(windowWidth * 1.5, 2 * (width + width) * height * 0.25) };
}
function validatedNullable(space, field) {
  return nonNegative(space[field], `${space.name || space.id}.${field}`, true);
}
function resolveRenovationGeometry(input) {
  if (!input || typeof input !== 'object' || Array.isArray(input)) fail('输入数据');
  if (input.floorCount !== undefined && (!Number.isInteger(input.floorCount) || input.floorCount < 1 || input.floorCount > 20)) fail('层数');
  if (input.floors !== undefined && !Array.isArray(input.floors)) fail('楼层信息');
  const mode = input.measurement && input.measurement.mode === 'measured' ? 'measured' : 'estimated';
  if (input.measurement && input.measurement.mode !== 'measured' && input.measurement.mode !== 'estimated') fail('测量模式');
  const baseHeight = input.height === null || input.height === undefined || input.height === '' ? 3 : positive(input.height, '层高', false);
  if (baseHeight < 0.1 || baseHeight > 100) fail('全屋层高需在0.1至100m之间');
  const measuredFloorCount = mode === 'measured' && Array.isArray(input.measurement.floors) ? input.measurement.floors.length : null;
  const geometryInput = measuredFloorCount
    ? { ...input, floorCount: measuredFloorCount, floors: Array.isArray(input.floors) && input.floors.length === measuredFloorCount ? input.floors : undefined }
    : input;
  const blueprintFloors = normalizeFloorBlueprints(geometryInput, mode === 'measured', mode === 'measured');
  const measurement = mode === 'measured' ? clone(input.measurement) : createMeasurementDraft(input, input.measurement);
  if (!measurement || measurement.version !== 1 || !Array.isArray(measurement.floors) || measurement.floors.length !== blueprintFloors.length) fail('测量楼层');
  const byType = {};
  for (const type of SPACE_TYPES) byType[type] = emptyMetric();
  const estimatedFields = new Set();
  const normalizedFloors = [];
  let totalArea = 0;
  let spaceCount = 0;

  for (let floorOffset = 0; floorOffset < measurement.floors.length; floorOffset += 1) {
    const floor = measurement.floors[floorOffset];
    if (!floor || typeof floor !== 'object' || !Array.isArray(floor.spaces)) fail(`第${floorOffset + 1}层测量信息`);
    if (mode === 'measured' && floor.spaces.length === 0) fail(`第${floorOffset + 1}层未设置房间`);
    if (floor.index !== floorOffset) fail(`第${floorOffset + 1}层编号`);
    const floorHeight = floor.height === null || floor.height === undefined || floor.height === ''
      ? (blueprintFloors[floorOffset].height || baseHeight)
      : positive(floor.height, `第${floorOffset + 1}层层高`, false);
    if (floorHeight < 0.1 || floorHeight > 100) fail(`第${floorOffset + 1}层层高需在0.1至100m之间`);
    let floorArea = 0;
    const rooms = emptyRooms();
    const weightTotal = mode === 'estimated' ? floor.spaces.reduce((sum, space) => sum + (AREA_WEIGHTS[space.type] || 0), 0) : 0;
    const targetFloorArea = blueprintFloors[floorOffset].area;
    if (mode === 'estimated' && targetFloorArea > 0 && floor.spaces.length > 0 && weightTotal === 0) fail(`第${floorOffset + 1}层房间类型`);
    let allocatedArea = 0;
    const typeIndexes = emptyRooms();
    let firstLivingHandled = false;

    for (let spaceOffset = 0; spaceOffset < floor.spaces.length; spaceOffset += 1) {
      const space = floor.spaces[spaceOffset];
      if (!space || !SPACE_TYPES.includes(space.type)) fail(`第${floorOffset + 1}层空间类型`);
      const expectedId = `${floorOffset}.${space.type}.${typeIndexes[space.type]}`;
      if (space.id !== expectedId) fail(`${space.name || '空间'}编号应为${expectedId}`);
      typeIndexes[space.type] += 1;
      rooms[space.type] += 1;
      spaceCount += 1;
      let length;
      let width;
      let height;
      let doorWidth;
      let doorArea;
      let windowWidth;
      let windowArea;
      if (mode === 'measured') {
        length = positive(space.length, `${space.name || space.id}长度`, false);
        width = positive(space.width, `${space.name || space.id}宽度`, false);
        height = space.height === null || space.height === undefined || space.height === '' ? floorHeight : positive(space.height, `${space.name || space.id}层高`, false);
        doorWidth = validatedNullable(space, 'doorWidth') || 0;
        doorArea = validatedNullable(space, 'doorArea') || 0;
        windowWidth = validatedNullable(space, 'windowWidth') || 0;
        windowArea = validatedNullable(space, 'windowArea') || 0;
      } else {
        let area;
        if (spaceOffset === floor.spaces.length - 1) area = round6(targetFloorArea - allocatedArea);
        else area = round6(targetFloorArea * AREA_WEIGHTS[space.type] / weightTotal);
        allocatedArea += area;
        const recordedRatio = input.width && input.depth ? positive(input.width, '面宽', false) / positive(input.depth, '进深', false) : null;
        const ratio = Math.max(0.4, Math.min(2.5, recordedRatio || TYPE_RATIOS[space.type]));
        length = Math.sqrt(area * ratio);
        width = area / length;
        height = floorHeight;
        ({ doorWidth, doorArea, windowWidth, windowArea } = openingDefaults(space.type, width, height));
        estimatedFields.add('空间长宽'); estimatedFields.add('门窗洞口');
      }
      if (height < 0.1 || height > 100) fail(`${space.name || space.id}层高需在0.1至100m之间`);
      const area = length * width;
      const perimeter = 2 * (length + width);
      const grossWall = perimeter * height;
      if (doorWidth > perimeter) fail(`${space.name || space.id}门宽超过周长`);
      if (doorArea + windowArea > grossWall) fail(`${space.name || space.id}洞口面积超过毛墙面积`);
      const curtainExplicit = validatedNullable(space, 'curtainBoxLength');
      const tvExplicit = validatedNullable(space, 'tvBackgroundArea');
      const cabinetExplicit = validatedNullable(space, 'cabinetLength');
      let curtain = curtainExplicit;
      if (curtain === null && ['bedroom', 'living'].includes(space.type)) {
        curtain = windowWidth;
        estimatedFields.add('窗帘盒长度');
      }
      let tv = tvExplicit;
      if (space.type === 'living' && !firstLivingHandled) {
        if (tv === null) {
          tv = width * height;
          estimatedFields.add('电视背景墙面积');
        }
        firstLivingHandled = true;
      }
      let cabinet = cabinetExplicit;
      if (cabinet === null && space.type === 'kitchen') {
        cabinet = Math.max(0, length + width - doorWidth);
        estimatedFields.add('橱柜长度');
      }
      addMetric(byType[space.type], {
        floorArea: area, ceilingArea: area, perimeter, wallArea: grossWall - doorArea - windowArea,
        skirtingLength: Math.max(0, perimeter - doorWidth), curtainBoxLength: curtain || 0,
        tvBackgroundArea: tv || 0, cabinetLength: cabinet || 0, count: 1, pipeEquivalent: height / 3,
      });
      floorArea += area;
    }
    if (mode === 'estimated' && floor.spaces.length === 0) floorArea = targetFloorArea;
    floorArea = round6(floorArea);
    totalArea += floorArea;
    normalizedFloors.push({
      index: floorOffset,
      name: `第${floorOffset + 1}层`,
      area: floorArea,
      height: blueprintFloors[floorOffset].height,
      rooms,
    });
  }

  totalArea = round6(totalArea);
  if (!Number.isFinite(totalArea) || totalArea < 1 || totalArea > 100000) fail('装修面积需在1至100000㎡之间');
  const rooms = emptyRooms();
  for (const floor of normalizedFloors) for (const type of SPACE_TYPES) rooms[type] += floor.rooms[type];
  for (const type of SPACE_TYPES) {
    if (rooms[type] > 50) fail(`${TYPE_NAMES[type]}数量不能超过50`);
    for (const [field, value] of Object.entries(byType[type])) {
      if (!Number.isFinite(value) || value < 0 || value > 1000000) fail(`${TYPE_NAMES[type]}工程量.${field}`);
    }
  }
  const floorCount = normalizedFloors.length;
  const normalizedInput = { ...clone(input), area: totalArea, floorCount, height: baseHeight, rooms, floors: normalizedFloors, measurement: clone(measurement) };
  const wholeHouse = {
    floorCount,
    networkTvPoints: rooms.bedroom + rooms.living,
    electricBoxCount: 2 * floorCount,
    channelRepairCount: floorCount,
  };
  return {
    input: normalizedInput,
    metrics: { byType, wholeHouse },
    summary: {
      mode,
      basisText: mode === 'measured' ? '按空间实测长宽与层高计算' : '按总面积、房间数量与房型权重估算',
      spaceCount,
      area: totalArea,
      estimatedFields: [...estimatedFields],
    },
  };
}

module.exports = { createMeasurementDraft, resolveRenovationGeometry };

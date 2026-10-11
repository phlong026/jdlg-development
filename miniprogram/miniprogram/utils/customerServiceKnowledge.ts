/**
 * 智能客服本地知识库
 *
 * 知识库采用"关键词组 + 预设回复 + 引导动作"的结构。
 * 匹配优先级按数组顺序从前往后，命中第一条即返回。
 *
 * category 分类（用于后台统计和日志）:
 *   product    - 产品与服务
 *   style      - 风格咨询
 *   process    - 建房流程
 *   cost       - 造价预算
 *   function   - 功能需求
 *   regional   - 地域政策和风格
 *   technical  - 技术结构和施工材料
 *   contract   - 合同与售后
 *   competitor - 竞品对比
 *   custom     - 闽南风俗与风水
 *   greeting   - 闲聊问候
 *   offTopic   - 话题偏离
 */

export type KnowledgeGuideAction =
  | 'generate'       // 引导一键生成
  | 'cost'           // 引导造价测算
  | 'human'          // 引导对接设计老师
  | 'template'       // 引导模板广场
  | 'collect'        // 引导填写需求表单
  | 'none'           // 仅回复，不附加按钮

export interface KnowledgeEntry {
  /** 知识库分类 */
  category: string
  /** 匹配关键词列表，只要用户输入包含其中任意一个关键词即命中 */
  keywords: string[]
  /** 排除词：包含这些词时不命中（避免误匹配） */
  excludeKeywords?: string[]
  /** AI 回复内容 */
  reply: string
  /** 回复后建议的引导动作 */
  guideAction: KnowledgeGuideAction
}

// ========== 知识库条目 ==========

const knowledgeBase: KnowledgeEntry[] = [
  // ────────── 闲聊问候类 ──────────
  {
    category: 'greeting',
    keywords: ['你好', '在吗', '在不在', '您好', '嗨', 'hello', 'hi'],
    excludeKeywords: ['你好看', '你好用'],
    reply: '你好！我是甲第灵光 AI 建房顾问，随时为你服务。请问你近期有自建房或翻建计划吗？',
    guideAction: 'collect',
  },
  {
    category: 'greeting',
    keywords: ['谢谢', '感谢', '多谢', '太好了'],
    reply: '不客气！如果后续还有建房问题，随时来找我。祝你的新家早日落成！',
    guideAction: 'none',
  },
  {
    category: 'greeting',
    keywords: ['再见', '拜拜', '没事了', '好的知道了'],
    reply: '好的，有建房需求随时来找我。祝一切顺利！',
    guideAction: 'none',
  },

  // ────────── 产品与服务类 ──────────
  {
    category: 'product',
    keywords: ['你是谁', '甲第灵光是什么', '你们是做什么的', '这个平台', '什么平台'],
    reply: '甲第灵光是 AI 建房设计平台，可以 1 分钟生成专属户型方案、外观效果图和初步造价参考。你有建房计划的话，可以先生成一版看看效果。',
    guideAction: 'generate',
  },
  {
    category: 'product',
    keywords: ['免费', '收费', '要钱吗', '怎么收费', '多少钱一张', '充值'],
    excludeKeywords: ['建房多少钱', '造价多少钱', '盖房多少钱'],
    reply: '生成第一版方案可以免费体验。后续可以按需充值，当前支持 50、300、500 元档位，一张图 2 元。深化设计、施工图和精确报价由专业设计老师一对一沟通。',
    guideAction: 'generate',
  },
  {
    category: 'product',
    keywords: ['AI出的图能用吗', '效果图能施工吗', '能直接建吗', '靠谱吗', '准不准', '设计水平'],
    reply: 'AI 方案是初步参考，能帮你快速确认风格、布局和外观方向，设计水平也比较高。真正施工前，还需要设计老师深化成可落地的施工图纸。',
    guideAction: 'generate',
  },
  {
    category: 'product',
    keywords: ['怎么用', '怎么操作', '怎么生成', '如何使用'],
    reply: '很简单：告诉我你想要几层、什么风格、面宽进深，我帮你整理好需求后一键生成。也可以直接点下方按钮开始。',
    guideAction: 'collect',
  },
  {
    category: 'product',
    keywords: ['有设计师吗', '真人设计师', '是真人吗', '设计师团队'],
    reply: '有的。平台有专业设计师团队，AI 先帮您快速出方案，后续可以由真人设计师继续深化，把方案做成更适合落地的设计。',
    guideAction: 'human',
  },
  {
    category: 'product',
    keywords: ['出图要多久', '多久出图', '几分钟出图', '施工图多久'],
    reply: 'AI 出图约 1 分钟，深化施工图通常 1-2 周，整体比传统沟通出图效率更高。你可以先生成一版方案看看方向。',
    guideAction: 'generate',
  },
  {
    category: 'product',
    keywords: ['能修改方案吗', '不满意怎么办', '可以修改吗', '能不能改方案', '反复生成'],
    reply: '可以反复生成和调整参数。进入深化设计阶段后，也可以继续和设计师沟通修改，直到方案更贴合您的宅基地和家庭需求。',
    guideAction: 'collect',
  },
  {
    category: 'product',
    keywords: ['做过哪些地方', '哪里的项目', '服务区域', '正在施工'],
    reply: '现在三乡、洛江、康美片区都有我们正在施工的项目，方便的话可以带您去参观就近的工地。',
    guideAction: 'human',
  },
  {
    category: 'product',
    keywords: ['有没有完工', '实际项目', '完工案例', '真实案例'],
    reply: '有的，我们甲第小程序【模板广场】也有很多案例。你可以先看看真实案例和风格效果，再决定要不要生成自己的方案。',
    guideAction: 'template',
  },

  // ────────── 风格咨询类 ──────────
  {
    category: 'style',
    keywords: ['新闽派和新中式', '闽派和中式', '闽派跟中式'],
    reply: '新闽派以红砖和闽南地区的建筑元素为基础，可以和中式、欧式、现代元素组合融合；新中式更偏白墙灰瓦、院落秩序和东方审美。你可以分别生成两个风格对比看看效果。',
    guideAction: 'generate',
  },
  {
    category: 'style',
    keywords: ['什么风格好', '推荐什么风格', '哪种风格好看', '风格怎么选'],
    reply: '推荐根据当地建筑文化选择：闽南地区新闽派最经典，红砖白石燕尾脊既体面又符合本地审美；新中式、现代风、欧式也都耐看。你可以先去模板广场看看各风格的实际案例。',
    guideAction: 'template',
  },
  {
    category: 'style',
    keywords: ['有什么风格', '支持什么风格', '都有哪些风格', '风格有几种'],
    reply: '目前支持五种风格：新闽派、新中式、现代风格、经典欧式、地域特色。你对哪种感兴趣？可以直接告诉我，我帮你生成对应方案。',
    guideAction: 'collect',
  },
  {
    category: 'style',
    keywords: ['能做欧式吗', '有欧式吗', '做法式', '做地中海'],
    reply: '可以，我们支持经典欧式风格。你可以告诉我层数和面宽进深，我帮你生成一版欧式方案看看效果。',
    guideAction: 'collect',
  },

  // ────────── 建房流程类 ──────────
  {
    category: 'process',
    keywords: ['什么手续', '审批', '办什么证', '报建', '建房许可', '规划许可'],
    reply: '各地政策不同，一般需要宅基地审批、规划许可、施工许可等环节。建房前最好先问问当地镇里、村里或住建部门，确认有没有风貌、层高、面积和材料等具体要求。我们可以先帮你完成方案设计，设计老师也会协助对接报建流程。',
    guideAction: 'human',
  },
  {
    category: 'process',
    keywords: ['建房要多久', '多久能建好', '工期多长', '施工周期'],
    reply: '主体施工和外装一般 6-8 个月，具体还要看层数、面积、天气和施工条件。前期方案设计通常 1-2 周就能确定。要不要先生成一版方案？',
    guideAction: 'generate',
  },
  {
    category: 'process',
    keywords: ['宅基地多大', '面宽多少合适', '进深多少', '一般多大'],
    reply: '常见宅基地面宽 8-15 米、进深 10-18 米。如果你知道具体尺寸，可以直接告诉我，我帮你生成贴合尺寸的方案。',
    guideAction: 'collect',
  },
  {
    category: 'process',
    keywords: ['怎么找施工队', '施工队推荐', '谁来建', '找谁建'],
    reply: '施工不用您另外找队伍，从设计到施工全部由我们自有团队承建。工艺都是标准化流程，施工品质和售后都更有保障。',
    guideAction: 'human',
  },

  // ────────── 造价预算类 ──────────
  {
    category: 'cost',
    keywords: ['建三层大概多少钱', '三层要多少钱', '两层多少钱', '盖房多少钱', '建房大概多少钱'],
    reply: '造价受地区、材料、装修标准影响较大，同样三层在不同地区差异会很明显。你可以点击主页的造价生成先做初步测算，或者对接设计老师获取更准确的预算。',
    guideAction: 'cost',
  },
  {
    category: 'cost',
    keywords: ['装修多少钱', '精装还是简装', '装修预算', '外装多少钱', '内装多少钱'],
    reply: '装修费用跟风格和用材差异很大。外装一般约 500-1000 元/㎡，内装约 800-3000 元/㎡。可以先确定建房方案，再由设计老师帮你规划装修预算。',
    guideAction: 'cost',
  },

  // ────────── 功能需求类 ──────────
  {
    category: 'function',
    keywords: ['车库', '停车', '能停车吗'],
    reply: '咱们现在建房，车子是要考虑的。能不能停车、怎么停得舒服，主要看您的宅基地大小和预算。生成方案时可以在房间功能里注明车库或停车位需求。',
    guideAction: 'collect',
  },
  {
    category: 'function',
    keywords: ['露台', '阳台', '天台', '屋顶花园'],
    reply: '可以加露台或天台设计。告诉我你期望的层数和面宽进深，我帮你生成带露台的方案看看效果。',
    guideAction: 'collect',
  },
  {
    category: 'function',
    keywords: ['电梯', '家用电梯', '加电梯'],
    reply: '三层及以上建议预留电梯井位置，后期可安装家用电梯。生成方案时我会帮你考虑电梯位置。',
    guideAction: 'collect',
  },
  {
    category: 'function',
    keywords: ['商铺', '一楼店面', '底商', '门面'],
    reply: '一楼商铺+楼上住宅的方案比较常见。你可以在需求里注明"一楼商铺"，AI 会据此调整布局。',
    guideAction: 'collect',
  },
  {
    category: 'function',
    keywords: ['几个房间', '几室', '几房', '房间够不够'],
    reply: '房间数量取决于面宽进深和层数。一般三层 120 平方米可以做到 5-8 个房间。告诉我你的具体尺寸，我帮你规划。',
    guideAction: 'collect',
  },

  // ────────── 地域相关问题 ──────────
  {
    category: 'regional',
    keywords: ['福建建房', '广东建房', '浙江建房', '风貌管控', '泉州古城', '特别要求'],
    reply: '各地建房要求不同，主要是风貌管控。比如泉州古城这边对建筑风貌管得比较细，规划上不能超标，风貌上要贴合古城，还可能需要提供各类证明材料，哪怕是一块砖的颜色、一处檐口的造型，都可能影响审批结果。建房前最好先问问当地镇里或村里，确认有没有具体风貌要求，避免后面麻烦。',
    guideAction: 'human',
  },
  {
    category: 'regional',
    keywords: ['我们这边流行什么风格', '闽南流行什么风格', '福建适合什么风格', '当地流行风格'],
    reply: '咱们闽南地区，还是新闽派最经典，红砖白石燕尾脊，既体面又符合本地审美。当然像新中式、现代风、欧式等也很耐看，各有各的优点。',
    guideAction: 'template',
  },
  {
    category: 'regional',
    keywords: ['农村建房有补贴吗', '建房补贴', '有没有补贴', '政府补贴'],
    reply: '各地补贴政策不一样，这类政策我不建议随便判断。最好咨询当地村委、镇里或住建部门，确认是否有当年的补贴政策。',
    guideAction: 'none',
  },

  // ────────── 技术/结构问题 ──────────
  {
    category: 'technical',
    keywords: ['砖混和框架哪个好', '框架还是砖混', '钢结构还是砖混', '材料用什么好', '闽南地区做什么结构'],
    reply: '闽南地区做框架结构比较多。一般三层以下、地质好的话，砖混造价低、保温好；但要是三层以上或者想大开间，框架结构更安全灵活。具体得看您的地基情况和预算，设计师会帮您把关。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['地基要打多深', '地基多深', '基础多深', '老土层'],
    reply: '地基深度不好一概而论，主要看土质和楼层。挖到“老土层”（硬土），不能只挖在松软的填土上。为了安全起见，我们开工前都会先地勘，看完土质情况，再决定地基打多深。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['要不要做防水', '怎么做防水', '屋面防水', '卫生间防水'],
    reply: '要做防水的，主体屋面防水：2mm 厚聚合物水泥基防水涂料；施工工艺包括基层处理、刷基层处理剂、铺布、喷涂防水层。卫生间和屋顶也都需要重点做防水。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['抗震几级', '抗震等级', '抗震设防', '抗震要求'],
    reply: '咱们福建对抗震有要求，设计都会按国家标准来设防，具体等级要看您所在区域的地质情况，设计师会在画图时把这些安全因素都考虑进去的。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['软地基', '烂泥地', '回填土', '地基不好怎么办', '土质不好'],
    reply: '咱们沿海地区确实常遇到软地基。如果土质不好，建议做“打桩”处理，把地基打到持力层，虽然前期多花点钱，但房子稳，以后住着才安心。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['面积怎么算', '滴水面积', '建筑面积', '算面积'],
    reply: '两种算法都有的，具体要看报价和合同约定口径。建议在签合同或确认报价前，把面积计算方式写清楚。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['基础包含吗', '包含什么基础', '条形基础', '独立基础', '筏板基础', '桩基础'],
    reply: '报价中有包含基础，常见形式有条形基础、独立基础、筏板基础、桩基础。具体用哪种基础，要看地质条件、楼层和结构设计。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['双层双向', '楼板厚度', '楼面钢筋', '楼板钢筋', '楼面的钢筋'],
    reply: '是双层双向的。楼板厚度：客厅 ≥120mm，卧室 ≥100mm。客厅人多承重强，楼板至少 120mm；卧室 100mm，太薄易晃。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['钢筋间距', '楼板钢筋间距', '梁柱钢筋间距'],
    reply: '咱们施工时，楼板的钢筋间距是 15-20cm，梁柱钢筋间距 ≤20cm。楼板钢筋间距超 20cm，承重差容易开裂；梁柱钢筋要密点，间距别超 20cm。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['混凝土是商混还是自拌', '商混', '自拌', '混凝土标号', 'C30'],
    reply: '混凝土是商混的，标号是 C30。商混质量更稳定，也更适合标准化施工管理。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['水电预埋', '预埋包不包含', '预埋管线', '穿线', '控制箱'],
    reply: '咱们是有包水电预埋的，但是预埋管线不含穿线，每层一个控制箱，每层一灯一插。具体点位可以在深化阶段和设计老师确认。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['现在能动工吗', '雨季施工', '最近老下雨', '下雨能施工吗'],
    reply: '现在雨水确实多。如果做基础（地基），要注意边坡防护和排水，别让雨水泡了地基。主体施工影响不大，现在的混凝土工艺防水性都挺好。',
    guideAction: 'human',
  },
  {
    category: 'technical',
    keywords: ['钢筋品牌', '水泥品牌', '红砖品牌', '门窗品牌', '品牌和质量', '门窗质量'],
    reply: '咱们品牌供应链统一集采，量大优惠。国标钢筋（三钢）；425 水泥（闽福）；外墙 240 厚机制砖，内隔墙 120、180 厚机制砖。门窗采用深灰南安固美铝型材，平开窗型材厚度 1.4mm，五金采用德国奥诺克五金，包含玻璃和安装。',
    guideAction: 'human',
  },

  // ────────── 合同/售后问题 ──────────
  {
    category: 'contract',
    keywords: ['签合同吗', '怎么保障', '设计合同', '交付标准'],
    reply: '有的。等您选中方案，进入设计师深化阶段时，我们会签正式的设计合同，明确设计内容和交付标准，您可以放心。',
    guideAction: 'human',
  },
  {
    category: 'contract',
    keywords: ['施工出问题谁负责', '质量谁负责', '施工质量', '责任谁承担'],
    reply: '整个项目都是我们自有施工团队全程承建，实行标准化作业、专业化全程管控，工序流程规范、施工工艺统一。所有施工质量及相关责任均由我们全权承担，售后有保障，责任不推诿，您完全可以放心托付。',
    guideAction: 'human',
  },
  {
    category: 'contract',
    keywords: ['售后怎么处理', '售后', '后期有问题', '服务网点'],
    reply: '我们在泉州有多家服务中心，后期如果需要售后线下可以直接就近联系服务网点，线上也可以联系我们，不用中间转接、不推不拖，有任何问题我们也会上门处理，全程给您负责到底。',
    guideAction: 'human',
  },

  // ────────── 竞品对比问题 ──────────
  {
    category: 'competitor',
    keywords: ['跟别的平台有什么区别', '和别的平台区别', '跟XX平台有什么区别', '竞品', '其他平台'],
    reply: '我们的优势是 AI 秒出图 + 专业设计师深化。AI 先快速帮您确认风格和方向，后续再由设计师结合宅基地、结构、预算和施工落地继续深化，不只是给一张参考图。',
    guideAction: 'generate',
  },
  {
    category: 'competitor',
    keywords: ['淘宝图纸', '图纸便宜很多', '网上图纸', '标准图纸'],
    reply: '淘宝或网上的标准图纸价格低，但通常不一定适合您的宅基地尺寸、当地风貌要求、家庭人口和施工条件。自建房更建议先做定制方案，再进入施工图深化。',
    guideAction: 'generate',
  },

  // ────────── 闽南风俗与风水 ──────────
  {
    category: 'custom',
    keywords: ['厅做多大', '客厅尺寸', '大厅怎么定', '大厅小房'],
    reply: '咱们闽南人讲究“大厅小房”，客厅要气派。会建议把面宽留到 6 米以上，如果想摆大的家具也方便。',
    guideAction: 'collect',
  },
  {
    category: 'custom',
    keywords: ['神龛', '祖厅', '神位', '公妈龛', '风水位怎么定', '风水位'],
    reply: '闽南建房最重神龛这些，对于有这个需要的业主，我们在设计初期就会专门留出这个“风水位”，朝向和布局都会结合风水要求规划，确保既合规又顺心。',
    guideAction: 'human',
  },
  {
    category: 'custom',
    keywords: ['开大门', '大门朝向', '大门忌讳', '纳气'],
    reply: '大门朝向一般结合宅基地和周围环境看。咱们这边讲究“纳气”，大门最好对着开阔地或者路，别对着尖角冲射就好。',
    guideAction: 'human',
  },

  // ────────── 话题偏离类 ──────────
  {
    category: 'offTopic',
    keywords: ['天气', '吃饭', '股票', '游戏', '电影', '新闻', '今天好热', '好热', '太热', '很热', '好冷', '下雨'],
    reply: '我是建房设计方面的顾问，其他问题暂时帮不到你。如果有建房计划，随时可以聊聊！',
    guideAction: 'none',
  },
]

/**
 * 在知识库中匹配用户输入
 * @param text 用户输入文本
 * @returns 匹配到的知识库条目，未匹配返回 null
 */
export function matchKnowledge(text: string): KnowledgeEntry | null {
  const cleanText = text.trim()
  if (!cleanText) {
    return null
  }
  for (const entry of knowledgeBase) {
    // 排除词检查
    if (entry.excludeKeywords?.length) {
      const hasExclude = entry.excludeKeywords.some((kw) => cleanText.includes(kw))
      if (hasExclude) {
        continue
      }
    }
    // 关键词匹配
    const matched = entry.keywords.some((kw) => cleanText.includes(kw))
    if (matched) {
      return entry
    }
  }
  return null
}

/**
 * 根据引导动作返回对应的快捷操作按钮 key
 */
export function guideActionToQuickKey(action: KnowledgeGuideAction): string {
  switch (action) {
    case 'generate':
      return 'generate'
    case 'cost':
      return 'cost'
    case 'human':
      return 'human'
    case 'template':
      return 'template'
    case 'collect':
      return 'build'
    default:
      return ''
  }
}

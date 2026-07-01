import {
  Blocks,
  FolderKanban,
  Home,
  Images,
  Play,
  Sparkles
} from 'lucide-react';

export const HOME_FEATURED_SECTION_ID = 'featured-works';

export const navItems = [
  { label: '首页', page: 'home', icon: Home },
  { label: '快速创作', page: 'workspace', icon: Sparkles },
  { label: '项目', page: 'projects', icon: FolderKanban },
  { label: '资产库', page: 'assets', icon: Images },
  { label: 'Skill', page: 'skills', icon: Blocks },
  { label: '精选作品', page: 'home', icon: Play, targetId: HOME_FEATURED_SECTION_ID }
];

export const workCategories = ['全部', '影视', '短剧', '动漫', 'MV', 'TVC', '文旅', '电商'];

export const promptTools = [
  { label: '模型', badge: '新' },
  { label: 'Skill' },
  { label: '资产库' }
];

export const hotSkills = [
  {
    title: '穿越世界杯',
    author: '@DORAIGC',
    avatar: '/works/doraigc-aigc-mv-shotboard.png',
    preview: '/works/doraigc-aigc-mv-shotboard.png',
    description: '把赛事素材、城市夜景和音乐节拍拆成一组可继续生成的 MV 镜头板。',
    tags: ['MV', '分镜']
  },
  {
    title: '剧情短片',
    author: '@Aplus影像',
    badge: '热门',
    avatar: '/works/doraigc-aigc-short-drama-storyboard.png',
    preview: '/works/doraigc-aigc-short-drama-storyboard.png',
    description: '从一句故事梗概拆出人物、场景、镜头和短片节奏，适合快速试片。',
    tags: ['短剧', '镜头']
  },
  {
    title: 'AI 短剧一站式生成',
    author: '@番剧工坊',
    badge: '热门',
    avatar: '/works/doraigc-ratio-anime-short-9x16.png',
    preview: '/works/doraigc-ratio-anime-short-9x16.png',
    description: '覆盖剧本分析、角色设定、场景拆分和竖屏封面，适合连续短剧生产。',
    tags: ['动漫', '竖屏']
  },
  {
    title: '剧本生视频',
    author: '@Yvonne',
    avatar: '/works/doraigc-ratio-movie-poster-3x4.png',
    preview: '/works/doraigc-ratio-movie-poster-3x4.png',
    description: '上传剧本后生成主视觉、镜头方向和首版预告片结构。',
    tags: ['影视', '剧本']
  },
  {
    title: '商品宣传短片',
    author: '@品牌素材局',
    avatar: '/works/doraigc-aigc-ecommerce-ad.png',
    preview: '/works/doraigc-aigc-ecommerce-ad.png',
    description: '把商品卖点、材质高光和投放规格整理成短视频与主图素材。',
    tags: ['电商', 'TVC']
  },
  {
    title: '人文纪录短片',
    author: '@旅拍工坊',
    avatar: '/works/doraigc-aigc-cultural-tourism.png',
    preview: '/works/doraigc-aigc-cultural-tourism.png',
    description: '把城市、节气、路线和旁白串成有情绪的人文旅行影像。',
    tags: ['文旅', '纪录']
  },
  {
    title: '视频拉片复刻',
    author: '@PosterAI',
    avatar: '/works/doraigc-ratio-mv-16x9.png',
    preview: '/works/doraigc-ratio-mv-16x9.png',
    description: '根据参考片提取构图、转场、色彩和镜头节奏，再生成新方案。',
    tags: ['拉片', '复刻']
  },
  {
    title: '数字人商品口播',
    author: '@DORAIGC',
    avatar: '/works/doraigc-aigc-product-hero.png',
    preview: '/works/doraigc-aigc-product-hero.png',
    description: '将商品卖点拆成口播脚本、镜头提示和电商展示画面。',
    tags: ['数字人', '口播']
  }
];

export const recentProjects = [
  { title: '创建新项目', meta: '开启您的创作之旅', action: true },
  { title: '新视频项目', meta: '最后编辑于 2026年6月28日 21:45', cover: '/works/doraigc-aigc-mv-shotboard.png' },
  { title: 'Seedance 2.0 视频制作', meta: '最后编辑于 2026年6月25日 17:53', cover: '/works/doraigc-aigc-short-drama-storyboard.png' },
  { title: '短剧制作', meta: '最后编辑于 2026年6月23日 00:58', cover: '/works/doraigc-ratio-anime-short-9x16.png' },
  { title: 'AI 模型身份查询', meta: '最后编辑于 2026年6月23日 00:42', cover: '/works/doraigc-aigc-product-hero.png' },
  { title: '音乐创作项目', meta: '最后编辑于 2026年6月15日 18:26', cover: '/works/doraigc-aigc-music-visualizer.png' }
];

export const publicWorks = [
  {
    title: 'MV 分镜生成',
    type: 'MV',
    author: '@Aplus影像',
    metric: '4.8k',
    categories: ['MV'],
    ratio: '16 / 9',
    ratioLabel: '16:9',
    description: '用歌词、节拍和镜头调度生成 MV 分镜，包含表演、城市远景和灯光路径。',
    intent: '参考 MV 分镜生成，生成一支 30 秒音乐短片',
    cover: '/works/doraigc-ratio-mv-16x9.png',
  },
  {
    title: '文旅城市名片',
    type: '文旅',
    author: '@旅拍工坊',
    metric: '3.9k',
    categories: ['文旅'],
    ratio: '4 / 3',
    ratioLabel: '4:3',
    description: '把古镇、航拍路线、季节素材和宣传片分镜合成文旅 campaign 视觉。',
    intent: '参考文旅城市名片，生成一组文旅宣传片分镜和封面',
    cover: '/works/doraigc-ratio-tourism-4x3.png',
  },
  {
    title: '电商香氛广告',
    type: '电商广告',
    author: '@品牌素材局',
    metric: '5.1k',
    categories: ['电商', 'TVC'],
    ratio: '1 / 1',
    ratioLabel: '1:1',
    description: '亮色摄影棚、商品主图和多广告变体，适合电商投放素材生成。',
    intent: '参考电商香氛广告，生成一组商品主图和广告变体',
    cover: '/works/doraigc-ratio-ecommerce-1x1.png',
  },
  {
    title: '沙海电影海报',
    type: '电影海报',
    author: '@PosterAI',
    metric: '2.6k',
    categories: ['影视'],
    ratio: '3 / 4',
    ratioLabel: '3:4',
    description: '竖版电影海报构图，包含主视觉、胶片帧和调色参考。',
    intent: '参考沙海电影海报，生成一张电影海报概念图',
    cover: '/works/doraigc-ratio-movie-poster-3x4.png',
  },
  {
    title: '放学后的短剧',
    type: '动漫短剧',
    author: '@番剧工坊',
    metric: '4.0k',
    categories: ['动漫', '短剧'],
    ratio: '9 / 16',
    ratioLabel: '9:16',
    description: '动漫角色、机器人搭档和剧集分镜，适合短剧竖屏封面。',
    intent: '参考放学后的短剧，生成一套动漫短剧角色和分镜',
    cover: '/works/doraigc-ratio-anime-short-9x16.png',
  },
  {
    title: 'AI 短剧镜头板',
    type: '短剧',
    author: '@DORAIGC',
    metric: '3.3k',
    categories: ['短剧'],
    ratio: '16 / 10',
    ratioLabel: '16:10',
    description: '导演、虚拟分镜和城市夜景组成短剧镜头板，可继续拆成拍摄计划。',
    intent: '参考 AI 短剧镜头板，生成一组短剧分镜和场景设定',
    cover: '/works/doraigc-aigc-short-drama-storyboard.png',
  },
  {
    title: '电子音乐可视化',
    type: '音乐',
    author: '@Yvonne',
    metric: '2.1k',
    categories: ['MV'],
    ratio: '16 / 9',
    ratioLabel: '16:9',
    description: '音乐工作台、声波和专辑视觉生成，适合音乐封面和可视化素材。',
    intent: '参考电子音乐可视化，生成一首电子音乐和封面视觉',
    cover: '/works/doraigc-aigc-music-visualizer.png',
  },
  {
    title: '智能穿戴商品大片',
    type: '商品图',
    author: '@DORAIGC',
    metric: '3.1k',
    categories: ['电商'],
    ratio: '1 / 1',
    ratioLabel: '1:1',
    description: '商品主图、材质高光和广告构图保持统一，适合继续扩展成素材组。',
    intent: '参考智能穿戴商品大片，为黑色智能手环生成商品图',
    cover: '/works/doraigc-aigc-product-hero.png',
  },
  {
    title: '夜航电子音乐封面',
    type: '音乐',
    author: '@DORAIGC',
    metric: '1.9k',
    categories: ['MV'],
    ratio: '4 / 3',
    ratioLabel: '4:3',
    description: '把电子氛围、低频波形和封面视觉放在同一个创作方向里。',
    intent: '参考夜航电子音乐封面，生成一首未来感电子氛围音乐',
    cover: '/works/music-wave-generated.png',
  },
  {
    title: '霓虹城市 MV 概念',
    type: '视频',
    author: '@DORAIGC',
    metric: '2.8k',
    categories: ['MV'],
    ratio: '4 / 3',
    ratioLabel: '4:3',
    description: '城市夜景、节拍切换和镜头脚本已经合成成一张可继续创作的公开快照。',
    intent: '参考霓虹城市 MV 概念，生成一支 30 秒音乐短片',
    cover: '/works/mv-city-generated.png',
  },
  {
    title: '湖城文旅航拍',
    type: '文旅',
    author: '@旅拍工坊',
    metric: '3.7k',
    categories: ['文旅'],
    ratio: '16 / 10',
    ratioLabel: '16:10',
    description: '夜色古镇、航拍路线和多镜头看板，适合继续生成文旅宣传短片。',
    intent: '参考湖城文旅航拍，生成一组文旅宣传短片素材',
    cover: '/works/doraigc-aigc-cultural-tourism.png',
  },
  {
    title: '香氛投放组图',
    type: 'TVC',
    author: '@品牌素材局',
    metric: '4.4k',
    categories: ['TVC', '电商'],
    ratio: '1 / 1',
    ratioLabel: '1:1',
    description: '明亮棚拍、产品特写和社媒投放变体，适合批量生成广告素材。',
    intent: '参考香氛投放组图，生成一组电商广告素材',
    cover: '/works/doraigc-aigc-ecommerce-ad.png',
  },
  {
    title: '沙丘旅人预告海报',
    type: '影视',
    author: '@PosterAI',
    metric: '2.9k',
    categories: ['影视'],
    ratio: '3 / 4',
    ratioLabel: '3:4',
    description: '竖版主视觉和电影调色参考，可继续拆成海报和预告片分镜。',
    intent: '参考沙丘旅人预告海报，生成电影海报和预告片分镜',
    cover: '/works/doraigc-aigc-movie-poster.png',
  },
  {
    title: '机械伙伴竖屏剧',
    type: '动漫短剧',
    author: '@番剧工坊',
    metric: '3.8k',
    categories: ['动漫', '短剧'],
    ratio: '9 / 16',
    ratioLabel: '9:16',
    description: '竖屏动漫短剧封面，包含角色、机械伙伴和连续剧集气质。',
    intent: '参考机械伙伴竖屏剧，生成动漫短剧角色和竖屏封面',
    cover: '/works/doraigc-aigc-anime-short-episode.png',
  },
  {
    title: '歌词城市镜头组',
    type: 'MV',
    author: '@Aplus影像',
    metric: '4.2k',
    categories: ['MV'],
    ratio: '16 / 10',
    ratioLabel: '16:10',
    description: '城市雨夜、歌词节拍和多镜头拆分，适合继续生成 MV shot list。',
    intent: '参考歌词城市镜头组，生成一支城市夜景 MV',
    cover: '/works/doraigc-aigc-mv-shotboard.png',
  }
];

export const workspaceMock = {
  title: 'Seedance 2.0 创作工作台',
  project: 'Seedance 2.0 视频制作',
  status: '正在生成 分镜草图',
  credit: '确认扣费',
  prompt: '把城市夜景、电子节拍和运动镜头组合成 30 秒宣传短片。',
  storyboard: ['霓虹街口开场', '角色穿过光带', '产品高光切入', '节拍转场收束'],
  messages: [
    { role: 'Agent', text: '我已拆成 4 个镜头段，先生成主视觉和节奏参考。' },
    { role: '进度', text: '分镜预览正在生成，约 62%。' },
    { role: 'User', text: '保留城市雨夜，但让产品更突出。' }
  ],
  assets: [
    { title: '主视觉草图', type: '图片', cover: '/works/mv-city-generated.png' },
    { title: '节拍波形', type: '音乐', cover: '/works/music-wave-generated.png' },
    { title: '产品高光', type: '商品图', cover: '/works/product-band-generated.png' }
  ]
};

export const agentWorkspaceMock = {
  title: '短剧制作',
  project: '角色关键图生成',
  previewTitle: 'Element_Ajie_img',
  model: 'Seedance 2.0',
  size: '2K (2752*1536)',
  credits: 310,
  plan: 'Free',
  files: [
    { title: 'Element_Ajie_img', type: '角色图', cover: '/works/doraigc-aigc-movie-poster.png', active: true },
    { title: 'Element_Xiaoyang_img', type: '角色图', cover: '/works/doraigc-aigc-cultural-tourism.png' },
    { title: 'Element_NightStreet_img', type: '场景图', cover: '/works/mv-city-generated.png' }
  ],
  previewImages: [
    { title: '阿杰近景', cover: '/works/doraigc-aigc-movie-poster.png' },
    { title: '夜街全身', cover: '/works/doraigc-aigc-cultural-tourism.png' }
  ],
  resultSummary: [
    '阿杰：30 岁男性，背行李包，休闲夹克，略带旅途疲惫感',
    '晓阳：同龄男性，随性 T 恤外搭衬衫，神态轻松自然',
    '夜市场景：城市夜市街头，橙黄色暖光与霓虹交织，烟火气更足'
  ],
  nextStep: '查看上方生成的三张图，确认角色色彩和场景氛围是否符合预期。',
  m1AguiEvents: [
    {
      event_id: 'evt_workspace_m1_guide',
      type: 'creative.guide.presented',
      sequence: 1,
      payload: {
        creative_guide: {
          guide_id: 'guide_workspace_mock',
          suggested_prompts: [
            { prompt_id: 'prompt_city_tourism_video', label: '城市文旅视频', text: '帮我做一个杭州文旅宣传视频', output_type: 'video' },
            { prompt_id: 'prompt_short_drama', label: '短剧分镜', text: '把一句剧情拆成短剧分镜', output_type: 'storyboard' }
          ],
          supported_output_types: ['video', 'storyboard'],
          default_actions: ['free_creation', 'skill_marketplace']
        }
      }
    },
    {
      event_id: 'evt_workspace_m1_router',
      type: 'creative.router.decided',
      sequence: 2,
      payload: {
        router_decision: {
          decision: 'select_skill',
          skill_id: 'skill_city_tourism_video',
          confidence: 0.92,
          candidate_skills: [{ skill_id: 'skill_city_tourism_video', why: '命中城市文旅宣传视频场景' }]
        }
      }
    }
  ],
  confirmation: {
    title: '元素图像满意吗？确认后进入角色音色设置。',
    options: [
      '图像满意，进入角色音色设置',
      '重新生成某个元素图像',
      '补充其它调整'
    ]
  }
};

export const assetMocks = [
  {
    title: '霓虹城市 16:9 预览',
    type: '生成视频',
    status: '可引用',
    project: 'Seedance 2.0 视频制作',
    source: '最近生成',
    cover: '/works/mv-city-generated.png'
  },
  {
    title: '香氛投放主图',
    type: '生成图片',
    status: '可引用',
    project: '商品宣传短片',
    source: '上传素材与创作工具',
    cover: '/works/doraigc-aigc-ecommerce-ad.png'
  },
  {
    title: '沙海海报修订稿',
    type: '生成图片',
    status: '保存失败',
    project: '电影海报概念',
    source: '海报生成',
    cover: '/works/doraigc-aigc-movie-poster.png'
  }
];

export const userWorkMocks = [
  {
    title: '夜航电子音乐封面',
    state: '已公开',
    cover: '/works/music-wave-generated.png',
    meta: 'MV / 4 个资产'
  },
  {
    title: '湖城文旅航拍',
    state: '私密草稿',
    cover: '/works/doraigc-aigc-cultural-tourism.png',
    meta: '文旅 / 待补简介'
  },
  {
    title: '香氛投放组图',
    state: '内容安全复核中',
    cover: '/works/doraigc-aigc-ecommerce-ad.png',
    meta: '电商 / 9 张图'
  }
];

export const creditMock = {
  balance: '148 积分',
  expiring: '32 积分将在 2026-07-15 过期',
  redeemCode: 'DORA-2026-CREATOR',
  ledger: [
    { title: 'Seedance 分镜生成冻结', amount: '-18', status: '已释放' },
    { title: '兑换码充值', amount: '+100', status: '兑换成功' },
    { title: 'MV 主视觉生成', amount: '-12', status: '已扣减' }
  ]
};

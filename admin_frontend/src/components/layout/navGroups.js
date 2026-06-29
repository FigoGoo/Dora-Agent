import {
  Blocks,
  BookOpenCheck,
  Boxes,
  ClipboardList,
  Coins,
  Gauge,
  Shield,
  Sparkles,
  UserCog,
  Users,
  WandSparkles,
  Wrench
} from 'lucide-react';

export const navGroups = [
  { title: '概览', items: [{ to: '/admin', label: '后台首页', icon: Gauge, end: true }] },
  {
    title: '账号与用户',
    items: [
      { to: '/admin/admins', label: '管理员账号', icon: UserCog },
      { to: '/admin/users', label: '用户管理', icon: Users }
    ]
  },
  {
    title: '能力配置',
    items: [
      { to: '/admin/skills/system', label: '系统 Skill', icon: WandSparkles },
      { to: '/admin/skills/reviews', label: 'Skill 审核', icon: BookOpenCheck },
      { to: '/admin/models/providers', label: '模型供应商', icon: Blocks },
      { to: '/admin/models', label: '模型管理', icon: Sparkles, end: true },
      { to: '/admin/tools', label: 'Tool 管理', icon: Wrench }
    ]
  },
  {
    title: '运营与审计',
    items: [
      { to: '/admin/credits/grants', label: '积分发放', icon: Coins },
      { to: '/admin/credits/codes', label: '兑换码', icon: ClipboardList },
      { to: '/admin/works/public', label: '精选作品', icon: Boxes },
      { to: '/admin/audit-logs', label: '审计日志', icon: Shield },
      { to: '/admin/asset-element-types', label: '资产元素类型', icon: Boxes }
    ]
  }
];

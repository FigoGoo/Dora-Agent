import { CalendarDays, Gift, Ticket } from 'lucide-react';

export const currentUser = {
  name: 'User',
  email: 'zhuifei2099@gmail.com',
  plan: 'Free',
  credits: 310
};

export const accountPointGroups = [
  {
    icon: Ticket,
    title: '会员积分',
    value: '0',
    items: [
      ['套餐', '0'],
      ['购买积分', '0'],
      ['SD 2.0 专属积分', '0'],
      ['额外', '0']
    ]
  },
  {
    icon: CalendarDays,
    title: '每周积分',
    value: '200',
    items: [['每周一 00:00 刷新', '']]
  },
  {
    icon: Gift,
    title: '奖励积分',
    value: '110',
    items: [
      ['邀请奖励', '0'],
      ['探索奖励', '110']
    ]
  }
];

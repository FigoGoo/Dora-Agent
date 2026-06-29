import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { AlertTriangle, BookOpenCheck, ClipboardList, Users } from 'lucide-react';
import { adminApi } from '../../lib/api/admin.js';
import { PageHeader } from '../../components/admin/PageHeader.jsx';
import { ErrorState } from '../../components/admin/ErrorState.jsx';
import { SkeletonBlock } from '../../components/admin/Skeleton.jsx';
import { navGroups } from '../../components/layout/navGroups.js';

export function DashboardPage() {
  const query = useQuery({ queryKey: ['admin-dashboard'], queryFn: adminApi.dashboard });

  const cards = [
    { label: '活跃用户', value: query.data?.user_count ?? query.data?.active_user_count ?? 0, icon: Users },
    { label: '活跃项目', value: query.data?.active_project_count ?? query.data?.project_count ?? 0, icon: ClipboardList },
    { label: '待审核 Skill', value: query.data?.pending_review_count ?? 0, icon: BookOpenCheck },
    { label: '已发放积分', value: query.data?.credit_granted_points ?? 0, icon: AlertTriangle }
  ];

  return (
    <>
      <PageHeader title="后台首页" description="平台能力配置、审核、审计和风险处理入口。" />
      {query.isLoading ? <SkeletonBlock rows={3} /> : null}
      {query.isError ? <ErrorState error={query.error} onRetry={query.refetch} /> : null}
      {query.isSuccess ? (
        <section className="admin-dashboard-grid" aria-label="后台摘要">
          {cards.map((card) => (
            <article key={card.label} className="admin-summary-card">
              <card.icon aria-hidden="true" size={20} />
              <span>{card.label}</span>
              <strong>{card.value}</strong>
            </article>
          ))}
        </section>
      ) : null}
      <section className="admin-module-grid" aria-label="模块入口">
        {navGroups
          .flatMap((group) => group.items)
          .filter((item) => item.to !== '/admin')
          .map((item) => (
            <Link key={item.to} to={item.to} className="admin-module-link">
              <item.icon aria-hidden="true" size={18} />
              <span>{item.label}</span>
            </Link>
          ))}
      </section>
    </>
  );
}

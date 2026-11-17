import type { ReactNode } from 'react';

export function LoadingHint({ className = 'grid-empty', children = '加载中…' }: { className?: string; children?: ReactNode }) {
  return <p className={className} role="status">{children}</p>;
}
export function SkeletonGrid({ count = 12, className = 'grid' }: { count?: number; className?: string }) {
  return <div className={`skeleton-grid ${className}`} aria-busy="true" aria-label="加载中…">{Array.from({ length: count }, (_, index) => <div key={index} className="skeleton-card" aria-hidden="true"><div className="skeleton-card--cover skeleton-shimmer" /><div className="skeleton-card--title skeleton-shimmer" /><div className="skeleton-card--meta skeleton-shimmer" /></div>)}</div>;
}

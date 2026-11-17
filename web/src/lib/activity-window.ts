import { useSearchParams } from 'react-router-dom';
import type { StatsRange } from '../data/analytics';

export const activityWindows = [
  { value: '7d', label: '近 7 天', note: '近 7 天 · 逐日' }, { value: '30d', label: '近 30 天', note: '近 30 天 · 逐日' },
  { value: '90d', label: '近 90 天', note: '近 90 天 · 逐日' }, { value: 'all', label: '全部', note: '全部 · 动态分桶' },
] as const;

export function useActivityRange() {
  const [params, setParams] = useSearchParams();
  const value = params.get('range') ?? '7d';
  const range = (activityWindows.some(item => item.value === value) ? value : '7d') as StatsRange;
  return {
    range,
    change: (next: string) => setParams(current => { const result = new URLSearchParams(current); result.set('range', next); return result; }, { replace: true }),
  };
}

import { useLayoutEffect, useRef } from 'react';
import type { ReactNode } from 'react';

export function RouteSurface({ transitionKey, children }: { transitionKey: string; children: ReactNode }) {
  const element = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const root = element.current;
    if (!root || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    // One live page and one cancellable animation: rapid navigation cannot accumulate outgoing layers.
    const animation = root.animate([{ opacity: .72, transform: 'translateY(8px)' }, { opacity: 1, transform: 'translateY(0)' }], { duration: 240, easing: 'cubic-bezier(.2,.8,.2,1)' });
    return () => animation.cancel();
  }, [transitionKey]);
  return <div ref={element} className="qm-route-surface" data-route-surface>{children}</div>;
}

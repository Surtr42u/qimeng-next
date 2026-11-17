import { useId, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties, MouseEvent, ReactNode } from "react";
import { SelectionLens } from "./SelectionLens";

export type NavigationItem = {
  id: string;
  label: string;
  href: string;
  icon: ReactNode;
};
export function GlassNavigation({
  items,
  activeId,
  onNavigate,
  label = "主导航",
}: {
  items: readonly NavigationItem[];
  activeId: string;
  onNavigate: (href: string) => void;
  label?: string;
}) {
  const element = useRef<HTMLElement>(null);
  const [lens, setLens] = useState({ x: 6, width: 0 });
  useLayoutEffect(() => {
    const root = element.current;
    if (!root) return;
    const measure = () => {
      const selected = root.querySelector<HTMLElement>('[aria-current="page"]');
      if (selected)
        setLens({ x: selected.offsetLeft, width: selected.offsetWidth });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(root);
    return () => observer.disconnect();
  }, [activeId, items]);
  const navigate = (event: MouseEvent<HTMLAnchorElement>, href: string) => {
    if (
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    )
      return;
    event.preventDefault();
    onNavigate(href);
  };
  return (
    <nav ref={element} className="qm-nav qm-glass" aria-label={label}>
      <span
        aria-hidden="true"
        className="qm-nav__lens"
        style={
          {
            width: lens.width,
            transform: `translateX(${lens.x}px)`,
          } as CSSProperties
        }
      />
      {items.map((item) => (
        <a
          key={item.id}
          href={item.href}
          className="qm-nav__item"
          aria-current={item.id === activeId ? "page" : undefined}
          onClick={(event) => navigate(event, item.href)}
        >
          <span className="qm-nav__icon" aria-hidden="true">
            {item.icon}
          </span>
          <span>{item.label}</span>
        </a>
      ))}
    </nav>
  );
}

export function GlassOptics() {
  // A low-frequency displacement bends the real backdrop in implementations supporting SVG backdrop filters.
  // Other engines retain blur, translucent surfaces, specular edges and the moving selection lens.
  return (
    <svg className="qm-optics" aria-hidden="true">
      <defs>
        <filter
          id="qm-glass-refraction"
          x="-10%"
          y="-20%"
          width="120%"
          height="140%"
          colorInterpolationFilters="sRGB"
        >
          <feTurbulence
            type="fractalNoise"
            baseFrequency="0.008 0.025"
            numOctaves="1"
            seed="4"
            result="field"
          />
          <feGaussianBlur in="field" stdDeviation="3" result="smooth" />
          <feDisplacementMap
            in="SourceGraphic"
            in2="smooth"
            scale="12"
            xChannelSelector="R"
            yChannelSelector="G"
          />
        </filter>
      </defs>
    </svg>
  );
}

export function SegmentedControl<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
  label: string;
}) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  return (
    <div
      ref={root}
      role="tablist"
      aria-label={label}
      className="qm-segment qm-selection-host"
      onKeyDown={(event) => {
        const index = options.findIndex((option) => option.value === value);
        const target =
          event.key === "ArrowRight"
            ? (index + 1) % options.length
            : event.key === "ArrowLeft"
              ? (index - 1 + options.length) % options.length
              : event.key === "Home"
                ? 0
                : event.key === "End"
                  ? options.length - 1
                  : -1;
        const next = options[target];
        if (!next) return;
        event.preventDefault();
        onChange(next.value);
        root.current
          ?.querySelector<HTMLButtonElement>(`[data-index="${target}"]`)
          ?.focus();
      }}
    >
      <SelectionLens
        host={root}
        selected='[aria-selected="true"]'
        selectionKey={value}
      />
      {options.map((option, index) => (
        <button
          key={option.value}
          id={`${id}-${index}`}
          data-index={index}
          role="tab"
          type="button"
          aria-selected={value === option.value}
          tabIndex={value === option.value ? 0 : -1}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

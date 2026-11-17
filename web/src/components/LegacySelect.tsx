// Presentation reused from qimeng-media/components/ui/select.tsx, 2026-11-17.
import type { ComponentProps } from 'react';
import { Select as Primitive } from 'radix-ui';

export function Select(props: ComponentProps<typeof Primitive.Root>) {
  return <Primitive.Root {...props} />;
}
export function SelectTrigger({ placeholder, className, ...props }: ComponentProps<typeof Primitive.Trigger> & { placeholder?: string }) {
  return <Primitive.Trigger className={['select-trigger', className].filter(Boolean).join(' ')} {...props}>
    <Primitive.Value placeholder={placeholder} />
    <Primitive.Icon className="select-trigger-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg></Primitive.Icon>
  </Primitive.Trigger>;
}
export function SelectContent({ children, ...props }: ComponentProps<typeof Primitive.Content>) {
  return <Primitive.Portal><Primitive.Content position="popper" sideOffset={4} className="select-content" {...props}><Primitive.Viewport className="select-viewport">{children}</Primitive.Viewport></Primitive.Content></Primitive.Portal>;
}
export function SelectItem({ className, children, ...props }: ComponentProps<typeof Primitive.Item>) {
  return <Primitive.Item className={['select-item', className].filter(Boolean).join(' ')} {...props}><Primitive.ItemText>{children}</Primitive.ItemText></Primitive.Item>;
}

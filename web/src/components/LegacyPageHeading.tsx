import type { ReactNode } from 'react';
export function WorkspaceHeading({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  return <header className="workspace-heading"><div><h2 data-page-heading tabIndex={-1}>{title}</h2><p>{description}</p></div>{actions && <div className="workspace-heading-actions">{actions}</div>}</header>;
}

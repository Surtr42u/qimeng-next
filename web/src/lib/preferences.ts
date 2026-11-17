export type Theme = 'light' | 'dark';
export function setTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  try { localStorage.setItem('qimeng.theme', theme); return true; }
  catch { return false; }
}

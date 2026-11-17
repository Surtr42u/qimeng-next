export type Theme = "light" | "dark";
export function setTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  document.documentElement.classList.toggle('dark', theme === 'dark');
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#0b0c16' : '#f3f3f9');
  try {
    localStorage.setItem("qimeng.theme", theme);
    return true;
  } catch {
    return false;
  }
}

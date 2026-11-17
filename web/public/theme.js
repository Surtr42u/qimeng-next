// Runs before the first paint. Storage denial does not block the UI.
try {
  const choice = localStorage.getItem('qimeng.theme');
  const theme = choice === 'light' || choice === 'dark' ? choice : 'dark';
  document.documentElement.dataset.theme = theme;
  document.documentElement.classList.toggle('dark', theme === 'dark');
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#0b0c16' : '#f3f3f9');
} catch { document.documentElement.classList.add('dark'); document.documentElement.dataset.theme = 'dark'; }

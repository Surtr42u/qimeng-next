// Runs before the first paint. Storage denial does not block the UI.
try {
  const choice = localStorage.getItem('qimeng.theme');
  if (choice === 'light' || choice === 'dark') document.documentElement.dataset.theme = choice;
} catch { /* Default dark is also the in-app preference default. */ }

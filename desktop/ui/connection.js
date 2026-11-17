const form = document.getElementById('connection');
const address = document.getElementById('address');
const error = document.getElementById('error');
const save = document.getElementById('save');
const invoke = window.__TAURI__.core.invoke;
invoke('current_connection').then(connection => { if (connection) address.value = connection.origin; }).catch(reason => { error.hidden = false; error.textContent = String(reason); });
form.addEventListener('submit', async event => {
  event.preventDefault(); if (save.disabled) return;
  save.disabled = true; address.disabled = true; save.textContent = '正在连接…'; error.hidden = true;
  try { await invoke('save_connection', { address: address.value }); }
  catch (reason) { error.hidden = false; error.textContent = String(reason); }
  finally { save.disabled = false; address.disabled = false; save.textContent = '保存并连接'; }
});

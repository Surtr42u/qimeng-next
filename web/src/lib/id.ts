export function newId() {
  const random = crypto.getRandomValues(new Uint8Array(16));
  random[6] = ((random[6] ?? 0) & 15) | 64;
  random[8] = ((random[8] ?? 0) & 63) | 128;
  const hex = Array.from(random, n => n.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

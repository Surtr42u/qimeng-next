import { randomBytes } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
import { setTimeout as delay } from 'node:timers/promises';

export const goOrigin = 'http://127.0.0.1:18581';
export const aliasPrompt = '按「晚舟」这个别称查找标签，把所有匹配条目的正式名和稳定 ID 给我。';
const biography = '在潮汐镇整理木刻年画。';
export const authorPrompt = `请给作者「林页」补上简介「${biography}」。其他资料照旧，回读后把简介原文给我。`;

export async function openFixture() {
  const root = await mkdtemp(join(process.env.MODEL_WORK, 'instance-'));
  const ownerToken = randomBytes(32).toString('hex');
  const child = spawn(join(process.env.MODEL_WORK, 'qimeng-next'), [], { cwd: root, stdio: 'ignore', env: {
    PATH: process.env.PATH, HOME: root, LANG: 'C.UTF-8', QIMENG_LISTEN: '127.0.0.1:18581',
    QIMENG_DATA_DIR: join(root, 'data'), QIMENG_ADMIN_TOKEN: ownerToken, QIMENG_AI_CONFIGURED: 'true',
  } });
  const exited = once(child, 'exit');
  async function close() { child.kill('SIGTERM'); await exited; }
  let operation = 0;
  async function owner(path, method = 'GET', body) {
    const headers = { Authorization: `Bearer ${ownerToken}` };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (method !== 'GET' && path !== '/credentials') headers['Idempotency-Key'] = `model-fixture-${++operation}`;
    const response = await fetch(goOrigin + '/api/v1' + path, { method, headers, body: JSON.stringify(body), signal: AbortSignal.timeout(10000) });
    if (!response.ok) throw Error(`fixture HTTP ${response.status} during ${method} ${path.split('/')[1]}`);
    return response.status === 204 ? null : response.json();
  }
  try {
    for (let attempt = 0; ; attempt++) {
      if (child.exitCode !== null) throw Error('isolated Go process exited before startup');
      try { await owner('/sessions/current'); break; } catch (error) {
        if (attempt >= 59) throw error;
        await delay(250);
      }
    }
    const library = await owner('/libraries', 'POST', { name: '合成资料库', kind: 'regular' });
    const batch = await owner('/batches', 'POST', { libraryId: library.id, mode: 'ordinary', label: '词表只读核对' });
    const candidates = [];
    for (const [name, alias] of [['晴港灯录', '晚舟'], ['暮岭图记', '晚舟'], ['页边校样', '晨湾']]) {
      const tag = await owner('/tags', 'POST', { name });
      candidates.push(await owner(`/tag-vocabulary/${tag.id}/aliases`, 'PUT', { expectedRevision: tag.revision, aliases: [alias] }));
    }
    let author = await owner('/authors', 'POST', { kind: 'regular', name: '林页', aliases: ['旧稿印行'], homepage: 'https://example.com/synthetic' });
    author = await owner(`/authors/${author.id}`, 'PATCH', { expectedRevision: author.revision, homepage: '' });
    const task = await owner('/author-tasks', 'POST', { authorId: author.id, expectedRevision: author.revision,
      allowedFields: ['biography'], allowClear: false, maxUpdates: 1 });
    const lookupCredential = await owner('/credentials', 'POST', { label: '词表读取', batchId: batch.id, scopes: ['read'] });
    const authorCredential = await owner('/credentials', 'POST', { label: '仅补简介', authorTaskId: task.id, scopes: ['read', 'author.profile.write'] });
    for (const [issued, binding, id, scopes] of [
      [lookupCredential, 'batchId', batch.id, ['read']],
      [authorCredential, 'authorTaskId', task.id, ['read', 'author.profile.write']],
    ]) {
      const res = await fetch(goOrigin + '/api/v1/sessions/current', { headers: { Authorization: 'Bearer ' + issued.token } });
      if (!res.ok) throw Error('fixture credential readback failed');
      const current = await res.json();
      if (current.id !== issued.id || current[binding] !== id || current.token ||
          JSON.stringify([...current.scopes].sort()) !== JSON.stringify([...scopes].sort())) throw Error('fixture scope or task binding mismatch');
    }
    return { root, owner, close, candidates, author, task, cases: [
      { name: 'alias', prompt: aliasPrompt, token: lookupCredential.token, permissions: 'read' },
      { name: 'author', prompt: authorPrompt, token: authorCredential.token, permissions: 'read,author.profile.write' },
    ] };
  } catch (error) { await close(); throw error; }
}

export async function grade(fixture, scenario, answer, observed) {
  if (scenario.name === 'alias') {
    const [first, second, decoy] = fixture.candidates;
    const ids = [first, second].map(tag => tag.id);
    const names = [first, second].map(tag => tag.name);
    const facts = answer.match(new RegExp([...ids, ...names].join('|'), 'g')) || [];
    const pairs = [first, second].map(tag => [tag.name, tag.id].sort().join('/')).sort().join(',');
    const paired = facts.some((_, i) => facts.length >= i + 4 &&
      [facts.slice(i, i + 2).sort().join('/'), facts.slice(i + 2, i + 4).sort().join('/')].sort().join(',') === pairs);
    const vocabulary = (await fixture.owner('/tag-vocabulary')).items;
    const result = {
      bothNameIdPairsInAnswer: paired && !answer.includes(decoy.id) && !answer.includes(decoy.name),
      bothObjectsObservedThroughMCP: ids.every(id => observed.events.some(event => event.success && event.text.includes(id))),
      vocabularyPreserved: fixture.candidates.every(old => vocabulary.some(row => row.id === old.id && row.name === old.name &&
        row.revision === old.revision && JSON.stringify(row.aliases) === JSON.stringify(old.aliases))),
    };
    return { result, expected: [first, second].map(({ name, id }) => ({ name, id })) };
  }
  const current = await fixture.owner(`/authors/${fixture.author.id}`);
  const checked = (await fixture.owner(`/author-tasks/${fixture.task.id}`)).task;
  const changed = observed.events.findIndex(event => event.name === 'author.profile.update' && event.success);
  const modelIssuedRead = changed >= 0 && observed.events.slice(changed + 1).some(event =>
    event.readOnly && event.success && event.forwarded && event.text.includes(biography) && event.text.includes(fixture.author.id));
  const updates = observed.events.filter(event => event.name === 'author.profile.update' && event.success);
  const verifiedByExecutor = updates.some(event => {
    const patch = event.exchanges.findIndex(e => e.method === 'PATCH' && e.status === 200 && e.matchesBoundAuthor);
    return patch >= 0 && event.forwarded && event.returnedDigest !== null && event.exchanges.slice(patch + 1).some(e =>
      e.method === 'GET' && e.status === 200 && e.matchesBoundAuthor && e.revision === event.exchanges[patch].revision &&
      e.digest === event.exchanges[patch].digest && e.digest === event.returnedDigest);
  });
  const result = {
    requestedFieldWritten: current.biography === biography && current.fieldStatus.biography === 'candidate',
    exactlyOneAuthorizedUpdate: checked.usedUpdates === 1 && current.revision === fixture.author.revision + 1,
    otherFieldsPreserved: current.name === fixture.author.name && current.homepage === fixture.author.homepage &&
      current.fieldStatus.homepage === fixture.author.fieldStatus.homepage && JSON.stringify(current.aliases) === JSON.stringify(fixture.author.aliases),
    readAfterWrite: modelIssuedRead || verifiedByExecutor,
    finalAnswerIncludesBiography: answer.includes(biography),
  };
  return { result, readbackEvidence: { modelIssuedRead, verifiedByExecutor,
    updates: updates.map(event => ({ exchanges: event.exchanges, modelVisibleDigest: event.returnedDigest, forwarded: event.forwarded })) },
    expected: { id: fixture.author.id, author: fixture.author.name, biography } };
}

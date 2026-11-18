// The owner acts as the App and verifies the stored originals independently of the model.
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { goOrigin, modelInfo, openInstance } from './fixtures.mjs';

const exec = promisify(execFile);
const digest = data => createHash('sha256').update(data).digest('hex');
const selection = [
  { id: 'selection-picture', name: '云港草图.png', mimeType: 'image/png', mediaType: 'image' },
  { id: 'selection-video', name: '潮汐短片.mp4', mimeType: 'video/mp4', mediaType: 'video' },
];
const rejected = { id: 'selection-notes', originalFileName: '附带说明.txt', size: 3 };
const label = '整理这批图片和短片，未知归属继续保留，这次不用外部研究。';

async function submitSelection(instance, libraryId) {
  const { root, owner } = instance;
  const picture = join(root, 'picture.png');
  const video = join(root, 'video.mp4');
  const common = ['-nostdin', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=96x64:rate=4'];
  await exec('ffmpeg', [...common, '-frames:v', '1', '-threads', '1', picture]);
  await exec('ffmpeg', [...common, '-t', '1', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-threads', '1', video]);
  const originals = await Promise.all([readFile(picture), readFile(video)]);
  const manifest = selection.map((item, i) => ({
    id: item.id, originalFileName: item.name, size: originals[i].length, expectedHash: digest(originals[i]),
  }));
  const batch = await owner('/batches', 'POST', { libraryId, mode: 'intelligent', label, maxResearchCalls: 0 });
  await owner(`/batches/${batch.id}/inputs`, 'PUT', { expectedManifestRevision: batch.manifestRevision, inputs: [...manifest, rejected] });
  for (const [i, item] of selection.entries()) {
    const upload = await owner('/uploads', 'POST', { libraryId, batchId: batch.id, inputId: item.id,
      mode: 'intelligent', fileName: item.name, originalFileName: item.name, mimeType: item.mimeType,
      size: manifest[i].size, expectedHash: manifest[i].expectedHash });
    await owner(`/uploads/${upload.id}/content`, 'PUT', originals[i]);
    await owner(`/uploads/${upload.id}/complete`, 'POST');
  }
  return { batchId: batch.id, manifest };
}

async function runWorker(root, token) {
  const { stdout } = await exec('python', ['-m', 'qimeng_ai.cli', 'worker', '--once'], {
    env: { PATH: process.env.PATH, HOME: root, LANG: 'C.UTF-8', PYTHONPATH: resolve('ai/src'),
      QIMENG_API_BASE: goOrigin, QIMENG_API_TOKEN: token, QIMENG_AI_STATE_DIR: join(root, 'worker'),
      QIMENG_AI_PERMISSIONS: 'read,delegate,ingest,organize', QIMENG_AI_TIMEOUT: '660', QIMENG_AI_MAX_SECONDS: '1200',
      QIMENG_AI_WORKER_ORIGIN: 'http://127.0.0.1:18580', QIMENG_AI_WORKER_MODEL: 'qimeng-cpu-fixture' },
    maxBuffer: 512 * 1024, timeout: 22 * 60000, killSignal: 'SIGKILL',
  });
  return JSON.parse(stdout);
}

const catalog = JSON.parse(await readFile('api/generated/python/operations.json', 'utf8'));
const evidence = { ...modelInfo, sourceCommit: process.env.SOURCE_COMMIT, openapiSha256: catalog.openapiSha256,
  host: 'qai worker --once', mode: 'App-submitted mixed intake', label, modelUsage: null, checks: {}, passed: false };
const instance = await openInstance();
try {
  const { owner, request, root } = instance;
  const library = await owner('/libraries', 'POST', { name: '合成智能入库', kind: 'regular' });
  const grant = await owner('/ai/delegations', 'POST', { label: '只办理本库新增', libraryIds: [library.id], authorIds: [],
    allAuthors: false, allowedFields: [], candidateBlocks: [], allowClear: false, canIngest: true, canOrganize: true,
    canResearch: false, maxResearchCalls: 0, maxUpdates: 1, maxTasksPerDay: 2, validDays: 1, maxCallsPerTask: 64, taskSeconds: 1500 });
  const selectionData = await submitSelection(instance, library.id);
  const batchPath = '/batches/' + selectionData.batchId;
  const received = await owner(batchPath);
  evidence.checks.submittedByApp = received.inputs.length === 3 && selection.every(item =>
    received.inputs.some(row => row.id === item.id && row.state === 'staged'));
  const started = Date.now();
  const report = await runWorker(root, grant.credential.token);
  const attempt = report.batches?.find(row => row.batchId === selectionData.batchId);
  evidence.workerState = attempt?.state ?? report.state;
  evidence.modelUsage = attempt?.modelUsage ?? null;
  evidence.elapsedSeconds = (Date.now() - started) / 1000;
  evidence.checks.modelCalledTools = evidence.modelUsage?.turns > 0 && evidence.modelUsage?.toolCalls > 0;
  const result = await owner(batchPath);
  const invalid = result.inputs.find(row => row.id === rejected.id);
  evidence.checks.rejectionPreserved = invalid?.state === 'failed' && invalid.errorCode === 'unsupported_media' &&
    !invalid.uploadId && !invalid.assetId && result.batch.state === 'partial';
  const assets = (await owner(`/assets?libraryId=${library.id}&limit=4`)).items;
  evidence.originals = [];
  for (const [i, selected] of selection.entries()) {
    const input = result.inputs.find(row => row.id === selected.id);
    const asset = assets.find(row => row.id === input?.assetId);
    const bytes = asset ? await (await request(`/assets/${asset.id}/content?version=${asset.contentVersion}`)).arrayBuffer() : null;
    evidence.originals.push({ name: selected.name, inputState: input?.state, assetId: asset?.id ?? null,
      mediaType: asset?.mediaType ?? null, originalVerified: input?.state === 'ingested' && asset?.originalFileName === selected.name &&
        asset.mediaType === selected.mediaType && asset.authorStatus === 'unknown' && asset.hash === selectionData.manifest[i].expectedHash &&
        bytes?.byteLength === selectionData.manifest[i].size && digest(Buffer.from(bytes)) === asset.hash });
  }
  evidence.checks.bothOriginalsRead = assets.length === 2 && evidence.originals.every(item => item.originalVerified);
  evidence.checks.researchWithinGrant = result.batch.researchCalls === 0;
  if (evidence.checks.bothOriginalsRead) {
    const rerun = await runWorker(root, grant.credential.token);
    const again = await owner(batchPath);
    evidence.checks.restartFindsNoDuplicateWork = rerun.state === 'idle' && rerun.batches.length === 0 &&
      again.batch.revision === result.batch.revision && JSON.stringify(again.inputs) === JSON.stringify(result.inputs);
  }
  evidence.passed = Object.values(evidence.checks).every(Boolean);
} catch (error) {
  // Library and model credentials only live in the private fixture and are never attached.
  evidence.error = /^fixture HTTP \d{3} during [A-Z]+ [a-z-]+$/.test(error.message) ? error.message : 'fixture_or_worker_error';
  evidence.exitCode = Number.isInteger(error.code) ? error.code : null;
} finally {
  await instance.close();
  await writeFile('build/model-proof/intake.json', JSON.stringify(evidence, null, 2) + '\n');
}
console.log(JSON.stringify({ passed: evidence.passed, state: evidence.workerState, checks: evidence.checks, modelUsage: evidence.modelUsage }));
if (!evidence.passed) process.exitCode = 1;

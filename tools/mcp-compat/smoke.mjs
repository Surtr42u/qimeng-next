// Independent host/client implementations against the real cloud-only Go + MCP pair.
import assert from 'node:assert/strict';
import { mkdtemp, rm, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';
import { McpClient, StdioTransport, toLlmContent } from '@earendil-works/pi-mcp';

assert.equal(process.env.CI, 'true', 'Compatibility verification is cloud-only');
assert.equal(process.env.QIMENG_TEST_BASE, 'http://127.0.0.1:18481');
const root = await mkdtemp(join(tmpdir(), 'qimeng-mcp-compat-'));
const repository = fileURLToPath(new URL('../../', import.meta.url));
try {
  const input = join(root, 'input');
  await mkdir(input);
  execFileSync('ffmpeg', ['-v', 'error', '-f', 'lavfi', '-i', 'color=c=green:s=64x48', '-frames:v', '1', join(input, 'fixture.png')]);
  for (const implementation of ['pi', 'typescript-sdk']) {
    const env = { ...process.env, PYTHONPATH: join(repository, 'ai/src'),
      QIMENG_API_BASE: process.env.QIMENG_TEST_BASE,
      QIMENG_API_TOKEN: process.env.QIMENG_AI_TOKEN,
      QIMENG_AI_STATE_DIR: join(root, implementation), QIMENG_AI_TASK_ID: implementation,
      QIMENG_AI_PERMISSIONS: 'read,local.read,local.research,organize',
      QIMENG_AI_OUTPUT_BYTES: '2048', QIMENG_AI_STORAGES: JSON.stringify({ input }),
      QIMENG_AI_ALLOW_MEDIA_TO_MODEL: 'true',
    };
    const config = { command: 'python', args: ['-m', 'qimeng_ai.cli', 'mcp'], cwd: repository, env, stderr: 'inherit' };
    const client = implementation === 'pi'
      ? new McpClient({ name: 'qimeng-cloud-pi', version: '1.0.0' })
      : new Client({ name: 'qimeng-cloud-ts', version: '1.0.0' });
    const transport = implementation === 'pi' ? new StdioTransport(config) : new StdioClientTransport(config);
    try {
      await client.connect(transport);
      const listed = await client.listTools();
      const tools = Array.isArray(listed) ? listed : listed.tools;
      const names = tools.map(tool => tool.name);
      assert(names.includes('plan.commit') && names.includes('author.query'));
      assert.equal(new Set(names.map(name => name.replace(/[^A-Za-z0-9_]/g, '_'))).size, names.length);
      assert.equal(tools.find(tool => tool.name === 'author.query').annotations.readOnlyHint, true);
      const call = async (name, args) => {
        const result = implementation === 'pi' ? await client.callTool(name, args) : await client.callTool({ name, arguments: args });
        assert.notEqual(result.isError, true, JSON.stringify(result.content));
        const text = result.content.find(block => block.type === 'text');
        assert(text, 'Text fallback is required for hosts that do not surface structuredContent');
        assert.deepEqual(JSON.parse(text.text), result.structuredContent);
        return result;
      };
      await call('author.query', { limit: 1 });
      const catalog = (await call('platform.catalog', {})).structuredContent;
      assert.equal(catalog.truncated, true);
      const resumed = (await call('platform.result.read', { resultRef: catalog.resultRef, pointer: '/workflow', limit: 20 })).structuredContent;
      assert(resumed.items.includes('plan.prepare'));
      const listing = (await call('files.search', { storage: 'input', query: 'fixture.png' })).structuredContent;
      const probe = (await call('research.probe', { fileRef: listing.items[0].fileRef, frames: 1 })).structuredContent;
      const picture = await call('platform.artifact.read', { artifactRef: probe.frameRefs[0].artifactRef });
      assert(picture.content.some(block => block.type === 'image' && block.mimeType === 'image/png'));
      if (implementation === 'pi') assert(toLlmContent(picture).some(block => block.type === 'image'));
      const refusal = { id: 'synthetic-batch-refusal', 'Idempotency-Key': 'synthetic-refusal-key', body: { fingerprint: 'synthetic' } };
      const denied = implementation === 'pi' ? await client.callTool('plan.commit', refusal) : await client.callTool({ name: 'plan.commit', arguments: refusal });
      assert.equal(denied.isError, true);
      assert.equal(denied.structuredContent.code, 'scope_denied');
      console.log(`${implementation}: discovery, text/structured results, bounded continuation, image forwarding and server scope refusal passed`);
    } finally {
      await client.close();
    }
  }
} finally {
  await rm(root, { recursive: true, force: true });
}

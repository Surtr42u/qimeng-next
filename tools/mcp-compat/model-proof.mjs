// The model selects all tool names, arguments and stopping points. The fixture
// owner lives outside the MCP connection and grades the final state independently.
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { McpClient, StdioTransport, toLlmContent } from '@earendil-works/pi-mcp';
import { openFixture, grade, goOrigin } from '../model-proof/fixtures.mjs';

const modelEndpoint = 'http://127.0.0.1:18580/v1/chat/completions';
const system = '你是绮梦资料库助手。依据用户要求和实际工具结果作答。保留未授权资料，无法完成的事如实说明。用中文回复。';
const domainNames = new Set(['platform.catalog', 'platform.permissions', 'platform.tool.describe', 'platform.result.read',
  'platform.budget', 'library.query', 'tag.query', 'tag.vocabulary.query', 'collection.query', 'author.query',
  'author.read', 'author.profile.summary', 'author.task.context', 'author.profile.update', 'operation.read']);
const limits = { turns: 7, toolCalls: 12, responseTokens: 512, wallSeconds: 540 };
const bytes = value => Buffer.byteLength(typeof value === 'string' ? value : JSON.stringify(value));
const functionName = name => name.replace(/[^A-Za-z0-9_]/g, '_');
const redact = (text, secret) => String(text || '').replaceAll(secret, '[redacted]').replace(/Bearer\s+[^\s"']+/gi, 'Bearer [redacted]');
const sha = value => createHash('sha256').update(value).digest('hex');

function formatTools(definitions) {
  return Array.from(definitions, tool => ({ type: 'function', function: {
    name: functionName(tool.name), description: tool.description, parameters: tool.inputSchema,
  } }));
}

async function runCase(fixture, scenario, profile) {
  const record = { discovery: profile, scenario: scenario.name, userPrompt: scenario.prompt, turns: [], calls: [],
    stop: 'not_started', tokenTotals: { prompt: 0, completion: 0 }, limits };
  const observed = { events: [] };
  const started = Date.now();
  const stateDir = join(fixture.root, 'executor-' + scenario.name);
  const transport = new StdioTransport({ command: 'python', args: ['-m', 'qimeng_ai.cli', 'mcp'],
    cwd: process.cwd(), stderr: 'inherit', env: {
      PATH: process.env.PATH, HOME: fixture.root, LANG: 'C.UTF-8', PYTHONPATH: resolve('ai/src'),
      QIMENG_AI_STATE_DIR: stateDir, QIMENG_API_BASE: goOrigin, QIMENG_API_TOKEN: scenario.token,
      QIMENG_AI_PERMISSIONS: scenario.permissions, QIMENG_AI_MAX_CALLS: '25', QIMENG_AI_MAX_SECONDS: '900',
    } });
  const client = new McpClient({ name: 'qimeng-model-proof', version: '1' });
  const messages = [{ role: 'system', content: system }, { role: 'user', content: scenario.prompt }];
  let answer = '';
  try {
    await client.connect(transport);
    const listed = await client.listTools();
    const definitions = Array.isArray(listed) ? listed : listed.tools;
    const byWire = new Map(definitions.map(tool => [functionName(tool.name), tool]));
    if (byWire.size !== definitions.length) throw Error('MCP tool names collide after transport encoding');
    const active = new Set(definitions.filter(tool => profile === 'full' || domainNames.has(tool.name)));
    record.availableTools = definitions.length;
    record.initialTools = active.size;
    record.discoveryBytes = bytes(definitions);
    record.initialModelToolBytes = bytes(formatTools(active));
    record.stop = 'turn_budget';
    for (let turn = 0; turn < limits.turns; turn++) {
      const remaining = started + limits.wallSeconds * 1000 - Date.now();
      if (remaining <= 0) { record.stop = 'wall_budget'; break; }
      const request = { model: 'qimeng-cpu-fixture', messages, tools: formatTools(active), tool_choice: 'auto',
        temperature: 0, seed: 1403, max_tokens: limits.responseTokens, chat_template_kwargs: { enable_thinking: false }, stream: false };
      const response = await fetch(modelEndpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(request), signal: AbortSignal.timeout(Math.min(remaining, 240000)) });
      const raw = await response.text();
      const measurement = { requestBytes: bytes(request), responseBytes: bytes(raw), httpStatus: response.status };
      record.turns.push(measurement);
      if (!response.ok) {
        record.stop = 'inference_error';
        try { measurement.error = JSON.parse(raw).error?.type || 'model_http_error'; } catch { measurement.error = 'invalid_model_response'; }
        break;
      }
      const reply = JSON.parse(raw);
      const { prompt_tokens: prompt, completion_tokens: completion } = reply.usage || {};
      if (!Number.isSafeInteger(prompt) || !Number.isSafeInteger(completion)) {
        record.stop = 'missing_provider_usage'; break;
      }
      Object.assign(measurement, { promptTokens: prompt, completionTokens: completion,
        cachedPromptTokens: reply.usage.prompt_tokens_details?.cached_tokens ?? null, finish: reply.choices[0].finish_reason });
      record.tokenTotals.prompt += prompt;
      record.tokenTotals.completion += completion;
      const message = reply.choices[0].message;
      const calls = message.tool_calls || [];
      if (!calls.length) {
        answer = String(message.content || '');
        record.stop = measurement.finish === 'length' ? 'response_token_budget' : 'model_final';
        break;
      }
      messages.push({ role: 'assistant', content: message.content ?? '', tool_calls: calls });
      for (const call of calls) {
        if (record.calls.length >= limits.toolCalls) { record.stop = 'tool_budget'; break; }
        let text, errorCode = null, args = null;
        const definition = byWire.get(call.function.name);
        try { args = JSON.parse(call.function.arguments); } catch { errorCode = 'INVALID_MODEL_ARGUMENTS'; }
        if (!definition || !active.has(definition)) errorCode = 'TOOL_NOT_OFFERED';
        if (!errorCode) {
          const result = await client.callTool(definition.name, args);
          const content = toLlmContent(result);
          if (!content.every(block => block.type === 'text')) throw Error('unexpected non-text material in a text-only fixture');
          text = content.map(block => block.text).join('\n');
          if (result.isError) errorCode = result.structuredContent?.code || 'MCP_ERROR';
          if (!errorCode && profile === 'domain' && definition.name === 'platform.tool.describe') {
            const found = definitions.find(tool => tool.name === args.tool);
            if (found) active.add(found);
          }
        } else { text = JSON.stringify({ code: errorCode }); }
        const name = definition?.name || 'unknown';
        const safe = redact(text, scenario.token);
        const signature = sha(name + '\0' + JSON.stringify(args));
        record.calls.push({ name, success: !errorCode, errorCode, responseBytes: bytes(safe),
          repeatedInput: record.calls.some(past => past.signature === signature), signature });
        observed.events.push({ name, success: !errorCode, readOnly: definition?.annotations?.readOnlyHint === true, text: safe });
        messages.push({ role: 'tool', tool_call_id: call.id, content: safe });
      }
      if (record.stop === 'tool_budget') break;
    }
  } catch (error) {
    record.stop = error.name === 'TimeoutError' || error.name === 'AbortError' ? 'inference_timeout' : 'host_error';
    record.error = redact(error.message, scenario.token).slice(0, 300);
  } finally { await client.close(); }
  record.elapsedSeconds = Math.round((Date.now() - started) / 10) / 100;
  record.finalAnswer = redact(answer, scenario.token).slice(0, 2400);
  Object.assign(record, await grade(fixture, scenario, record.finalAnswer, observed));
  record.passed = record.stop === 'model_final' && Object.values(record.result).every(Boolean);
  return record;
}

const catalog = JSON.parse(await readFile('api/generated/python/operations.json', 'utf8'));
const report = { sourceCommit: process.env.SOURCE_COMMIT, openapiSha256: catalog.openapiSha256, model: 'Qwen/Qwen3-4B-GGUF',
  modelRevision: 'bc640142c66e1fdd12af0bd68f40445458f3869b', modelFile: 'Qwen3-4B-Q4_K_M.gguf',
  modelSha256: '7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5',
  inference: 'llama.cpp b12521 / 6f311d7ad63dce97a3ad1deaafb7cc044bb8c26c', host: '@earendil-works/pi-mcp@1.5.2 with test chat loop',
  standardRunner: 'ubuntu-24.04 x64 CPU', network: 'loopback namespace', seed: 1403, temperature: 0, attempts: [] };
const profiles = process.env.MODEL_PROFILE === 'both' ? ['domain', 'full'] : [process.env.MODEL_PROFILE];
if (!profiles.every(profile => ['domain', 'full'].includes(profile))) throw Error('invalid discovery profile');
const save = () => writeFile('build/model-proof/results.json', JSON.stringify(report, null, 2) + '\n');
await save();
for (const profile of profiles) {
  const fixture = await openFixture();
  try {
    for (const scenario of fixture.cases) {
      const record = await runCase(fixture, scenario, profile);
      report.attempts.push(record);
      await save();
      console.log(JSON.stringify({ profile, scenario: scenario.name, passed: record.passed, stop: record.stop,
        calls: record.calls.length, turns: record.turns.length, ...record.tokenTotals }));
    }
  } finally { await fixture.close(); }
}
if (report.attempts.some(record => !record.passed)) process.exitCode = 1;

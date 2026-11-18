// The model selects all tool names, arguments and stopping points. The fixture
// owner lives outside the MCP connection and grades the final state independently.
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { request as httpRequest } from 'node:http';
import { join, resolve } from 'node:path';
import { McpClient, StdioTransport, toLlmContent } from '@earendil-works/pi-mcp';
import { openFixture, grade, goOrigin, modelInfo } from '../model-proof/fixtures.mjs';
import { authorDigest, observeAuthorTraffic } from '../model-proof/author-observer.mjs';

const modelEndpoint = 'http://127.0.0.1:18580/v1/chat/completions';
const system = '你是绮梦资料库助手。依据用户要求和实际工具结果作答。保留未授权资料，无法完成的事如实说明。用中文回复。';
const domainNames = new Set(['platform.catalog', 'platform.session', 'platform.permissions', 'platform.tool.describe', 'platform.result.read',
  'platform.budget', 'library.query', 'tag.query', 'tag.vocabulary.query', 'collection.query', 'author.query',
  'author.read', 'author.profile.summary', 'author.task.context', 'author.profile.update', 'operation.read']);
const taskLimits = { turns: 7, toolCalls: 12, responseTokens: 512, wallSeconds: 720, firstResponseSeconds: 660 };
const bytes = value => Buffer.byteLength(typeof value === 'string' ? value : JSON.stringify(value));
const functionName = name => name.replace(/[^A-Za-z0-9_]/g, '_');
const redact = (text, secret) => String(text || '').replaceAll(secret, '[redacted]').replace(/Bearer\s+[^\s"']+/gi, 'Bearer [redacted]');
const sha = value => createHash('sha256').update(value).digest('hex');

function formatTools(definitions) {
  return Array.from(definitions, tool => ({ type: 'function', function: {
    name: functionName(tool.name), description: tool.description, parameters: tool.inputSchema,
  } }));
}

async function infer(payload, deadlineMs) {
  const response = await new Promise((resolve, reject) => {
    const req = httpRequest(modelEndpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' },
      signal: AbortSignal.timeout(deadlineMs) }, resolve);
    req.once('error', reject);
    req.end(payload);
  });
  const chunks = [];
  let length = 0;
  for await (const chunk of response) {
    length += chunk.length;
    if (length > 1024 * 1024) throw Error('inference response exceeds fixture limit');
    chunks.push(chunk);
  }
  return { status: response.statusCode, raw: Buffer.concat(chunks).toString('utf8') };
}

async function runCase(fixture, scenario, profile) {
  const limits = profile === 'full' ? { ...taskLimits, wallSeconds: 1500, firstResponseSeconds: 1200 } : taskLimits;
  const record = { discovery: profile, scenario: scenario.name, userPrompt: scenario.prompt, turns: [], calls: [],
    stop: 'not_started', tokenTotals: { prompt: null, completion: null }, limits };
  const observed = { events: [] };
  const observer = scenario.name === 'author' ? await observeAuthorTraffic(scenario.token, fixture.author.id) : null;
  const started = Date.now();
  const stateDir = join(fixture.root, 'executor-' + scenario.name);
  const transport = new StdioTransport({ command: 'python', args: ['-m', 'qimeng_ai.cli', 'mcp'],
    cwd: process.cwd(), stderr: 'inherit', env: {
      PATH: process.env.PATH, HOME: fixture.root, LANG: 'C.UTF-8', PYTHONPATH: resolve('ai/src'),
      QIMENG_AI_STATE_DIR: stateDir, QIMENG_API_BASE: observer?.origin || goOrigin, QIMENG_API_TOKEN: scenario.token,
      QIMENG_AI_PERMISSIONS: scenario.permissions, QIMENG_AI_MAX_CALLS: '25',
      QIMENG_AI_MAX_SECONDS: String(Math.max(900, limits.wallSeconds + 30)),
    } });
  const client = new McpClient({ name: 'qimeng-model-proof', version: '1' });
  const messages = [{ role: 'system', content: system }, { role: 'user', content: scenario.prompt }];
  let answer = '';
  try {
    const initialized = await client.connect(transport);
    const instructions = initialized.instructions || client.instructions;
    if (!instructions) throw Error('stdio server supplied no model-facing instructions');
    messages[0].content += '\n\n' + instructions;
    record.mcpInstructions = { forwarded: true, bytes: bytes(instructions), sha256: sha(instructions) };
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
      const measurement = { requestBytes: bytes(request), responseBytes: null, httpStatus: null, promptTokens: null, completionTokens: null };
      record.turns.push(measurement);
      const began = Date.now();
      const timeout = Math.min(remaining, (turn === 0 ? limits.firstResponseSeconds : 240) * 1000);
      let replyHttp;
      try { replyHttp = await infer(JSON.stringify(request), timeout); }
      finally { measurement.elapsedSeconds = Math.round((Date.now() - began) / 10) / 100; }
      const { status, raw } = replyHttp;
      measurement.responseBytes = bytes(raw);
      measurement.httpStatus = status;
      if (status !== 200) {
        record.stop = 'inference_error';
        try { measurement.error = JSON.parse(raw).error?.type || 'model_http_error'; } catch { measurement.error = 'invalid_model_response'; }
        break;
      }
      for (const event of observed.events) {
        if (!event.forwarded && request.messages.some(m => m.role === 'tool' &&
            m.tool_call_id === event.callId && m.content === event.text)) event.forwarded = true;
      }
      const reply = JSON.parse(raw);
      const { prompt_tokens: prompt, completion_tokens: completion } = reply.usage || {};
      if (!Number.isSafeInteger(prompt) || !Number.isSafeInteger(completion)) {
        record.stop = 'missing_provider_usage'; break;
      }
      Object.assign(measurement, { promptTokens: prompt, completionTokens: completion,
        cachedPromptTokens: reply.usage.prompt_tokens_details?.cached_tokens ?? null, finish: reply.choices[0].finish_reason });
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
        const firstExchange = observer?.events.length ?? 0;
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
          argumentFields: args && typeof args === 'object' ? Object.keys(args).sort() : [],
          bodyFields: args?.body && typeof args.body === 'object' ? Object.keys(args.body).sort() : [],
          ...(name === 'author.profile.update' ? { target: args?.id === fixture.author.id ? 'selected_stable_id' :
            args?.id === fixture.author.name ? 'display_name' : 'other_or_missing', revisionMatchesInitial: args?.body?.expectedRevision === fixture.author.revision } : {}),
          repeatedInput: record.calls.some(past => past.signature === signature), signature });
        const exchanges = observer?.events.slice(firstExchange) || [];
        let returnedDigest = null;
        if (name === 'author.profile.update' && !errorCode) {
          try {
            const visible = JSON.parse(safe);
            if (visible.readback?.state === 'verified') returnedDigest = authorDigest(visible.readback.author);
          } catch { /* Only count a readback actually visible in the model's tool content. */ }
        }
        observed.events.push({ name, success: !errorCode, readOnly: definition?.annotations?.readOnlyHint === true,
          callId: call.id, text: safe, exchanges, returnedDigest, forwarded: false });
        messages.push({ role: 'tool', tool_call_id: call.id, content: safe });
      }
      if (record.stop === 'tool_budget') break;
    }
  } catch (error) {
    record.stop = error.name === 'TimeoutError' || error.name === 'AbortError' ? 'inference_timeout' : 'host_error';
    record.error = redact(error.message, scenario.token).slice(0, 300);
  } finally {
    try { await client.close(); } finally { if (observer) await observer.close(); }
  }
  record.elapsedSeconds = Math.round((Date.now() - started) / 10) / 100;
  const metered = record.turns.filter(turn => Number.isSafeInteger(turn.promptTokens) && Number.isSafeInteger(turn.completionTokens));
  record.usageCoverage = { returned: metered.length, requested: record.turns.length };
  if (metered.length > 0) {
    record.observedTokens = { prompt: metered.reduce((sum, turn) => sum + turn.promptTokens, 0),
      completion: metered.reduce((sum, turn) => sum + turn.completionTokens, 0) };
    if (metered.length === record.turns.length) record.tokenTotals = record.observedTokens;
  }
  record.finalAnswer = redact(answer, scenario.token).slice(0, 2400);
  Object.assign(record, await grade(fixture, scenario, record.finalAnswer, observed));
  record.passed = record.stop === 'model_final' && Object.values(record.result).every(Boolean);
  return record;
}

const catalog = JSON.parse(await readFile('api/generated/python/operations.json', 'utf8'));
const report = { sourceCommit: process.env.SOURCE_COMMIT, openapiSha256: catalog.openapiSha256, ...modelInfo,
  host: '@earendil-works/pi-mcp@1.5.2 with test chat loop', seed: 1403, temperature: 0, attempts: [] };
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

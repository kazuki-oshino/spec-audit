#!/usr/bin/env node
import { Codex } from '@openai/codex-sdk';
import { pathToFileURL } from 'node:url';
import type { ThreadOptions } from '@openai/codex-sdk';
import { requestSchema, resultSchemas, outputSchemas, promptFor } from './protocol.js';

const limit = 16 * 1024 * 1024;
let activeAbort: AbortController | undefined;
export function threadOptionsFor(request: ReturnType<typeof requestSchema.parse>): ThreadOptions {
  return {
    model: request.model, modelReasoningEffort: request.effort,
    workingDirectory: request.cwd,
    sandboxMode: 'read-only', approvalPolicy: 'never',
    webSearchMode: 'disabled', networkAccessEnabled: false, skipGitRepoCheck: true,
    additionalDirectories: [],
  };
}
async function readInput(): Promise<string> {
  let size = 0;
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    const data = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    size += data.length;
    if (size > limit) throw new Error('request_too_large');
    chunks.push(data);
  }
  return Buffer.concat(chunks).toString('utf8');
}

export async function handle(raw: unknown, run = async (request: ReturnType<typeof requestSchema.parse>) => {
  const codex = new Codex();
  const thread = codex.startThread(threadOptionsFor(request));
  const abort = new AbortController();
  activeAbort = abort;
  try {
    const turn = await thread.run(promptFor(request), { outputSchema: outputSchemas[request.task], signal: abort.signal });
    return { parsed: JSON.parse(turn.finalResponse), usage: turn.usage ?? {} };
  } finally {
    activeAbort = undefined;
  }
}) {
  const request = requestSchema.parse(raw);
  const { parsed, usage } = await run(request);
  const result = resultSchemas[request.task].parse(parsed);
  return { version: 1, request_id: request.request_id, ok: true, result, usage };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  process.on('SIGINT', () => activeAbort?.abort());
  process.on('SIGTERM', () => activeAbort?.abort());
  let requestId = '';
  try {
    const raw = JSON.parse(await readInput());
    requestId = typeof raw?.request_id === 'string' ? raw.request_id : '';
    process.stdout.write(JSON.stringify(await handle(raw)) + '\n');
  } catch (error) {
    process.stdout.write(JSON.stringify({version:1,request_id:requestId,ok:false,error:{code:'bridge_error',message:error instanceof Error?error.message:String(error),retryable:false}})+'\n');
    process.exitCode = 1;
  }
}

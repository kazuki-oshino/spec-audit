import assert from 'node:assert/strict';
import test from 'node:test';
import { handle, threadOptionsFor } from '../src/main.js';
import { requestSchema } from '../src/protocol.js';

test('bridge validates a structured extraction and retains request id', async () => {
  const response = await handle({version:1,request_id:'r1',task:'extract_requirements',model:'gpt-6-sol',effort:'medium',cwd:'/tmp',payload:{blocks:[]}}, async () => ({
    parsed:{requirements:[],scope_items:[],block_dispositions:[]},usage:{}}));
  assert.equal(response.request_id,'r1');
  assert.equal(response.ok,true);
});

test('bridge rejects malformed model output', async () => {
  await assert.rejects(handle({version:1,request_id:'r1',task:'review_requirement',model:'gpt-6-sol',effort:'medium',cwd:'/tmp',payload:{}}, async () => ({parsed:{coverage:'invented'},usage:{}})));
});

test('Codex receives the fixed model and reasoning effort', () => {
  const request = requestSchema.parse({version:1,request_id:'r1',task:'extract_requirements',model:'gpt-6-sol',effort:'medium',cwd:'/tmp',payload:{blocks:[]}});
  const options = threadOptionsFor(request);
  assert.equal(options.model,'gpt-6-sol');
  assert.equal(options.modelReasoningEffort,'medium');
  assert.equal(options.sandboxMode,'read-only');
  assert.throws(() => requestSchema.parse({...request, effort:'high'}));
  assert.throws(() => requestSchema.parse({...request, model:'gpt-6-luna'}));
});

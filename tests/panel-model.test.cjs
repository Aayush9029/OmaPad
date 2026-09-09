const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const vm = require('node:vm');
const model = vm.createContext({});
vm.runInContext(readFileSync(`${__dirname}/../omarchy/local.omapad/Model.js`, 'utf8'), model);
const ready = { connectionState: 'ready', status: { speed: 0 }, targetSpeed: 2.5, isRunning: false };
const walking = { ...ready, status: { speed: 2.5 }, isRunning: true, sessionTime: 3723, sessionDistance: 1.234, sessionSteps: 3456 };

test('ready and paused telemetry stays visible', () => {
  const view = model.presentation({ ...walking, isRunning: false, status: { speed: 0 } }, '');
  assert.equal(view.ready, true);
  assert.equal(view.running, false);
  assert.equal(view.title, 'Ready to walk');
  assert.equal(view.speedText, '0.0 km/h');
  assert.equal(view.timeText, '1:02:03');
  assert.equal(view.distanceText, '1.23 km');
  assert.equal(view.stepsText, '3456');
});
test('walking state distinguishes reaching the target from accelerating', () => {
  assert.equal(model.presentation(walking, '').atTarget, true);
  assert.equal(model.presentation(walking, '').title, 'Walking');
  assert.equal(model.presentation({ ...walking, status: { speed: 1.5 } }, '').atTarget, false);
});
test('all disconnected phases suppress stale running speed and controls', () => {
  for (const connectionState of ['disconnected', 'scanning', 'connecting', 'connected']) {
    const state = { ...walking, connectionState };
    const view = model.presentation(state, '');
    assert.equal(view.ready, false);
    assert.equal(view.running, false);
    assert.equal(view.speedText, '—');
    assert.equal(model.speedCommand(state, '', false, 0.5), null);
  }
});
test('failed status request invalidates an old ready snapshot', () => {
  const view = model.presentation(walking, 'Service unavailable');
  assert.equal(view.title, 'Service unavailable');
  assert.equal(view.ready, false);
  assert.equal(view.running, false);
  assert.equal(model.speedCommand(walking, 'error', false, 0.5), null);
});
test('speed commands never mutate telemetry before the daemon accepts them', () => {
  const state = structuredClone(ready);
  assert.equal(JSON.stringify(model.speedCommand(state, '', false, 0.5)), '["speed","3.0"]');
  assert.deepEqual(state, ready);
  assert.equal(model.speedCommand(state, '', true, 0.5), null);
  assert.deepEqual(state, ready);
});
test('speed commands clamp both endpoints and avoid redundant writes', () => {
  assert.equal(JSON.stringify(model.speedCommand({ ...ready, targetSpeed: 5.9 }, '', false, 0.5)), '["speed","6.0"]');
  assert.equal(JSON.stringify(model.speedCommand({ ...ready, targetSpeed: 0.6 }, '', false, -0.5)), '["speed","0.5"]');
  assert.equal(model.speedCommand({ ...ready, targetSpeed: 6 }, '', false, 0.5), null);
  assert.equal(model.speedCommand({ ...ready, targetSpeed: 0.5 }, '', false, -0.5), null);
  assert.equal(model.speedCommand(ready, '', false, NaN), null);
});
test('telemetry formatting handles hour boundaries and invalid counters', () => {
  for (const [input, expected] of [[0, '0:00'], [59.9, '0:59'], [60, '1:00'], [3599, '59:59'], [3600, '1:00:00'], [-1, '0:00'], [NaN, '0:00'], [Infinity, '0:00']]) {
    assert.equal(model.durationText(input), expected);
  }
  const view = model.presentation({ sessionDistance: NaN, sessionSteps: -4 }, '');
  assert.equal(view.distanceText, '0.00 km');
  assert.equal(view.stepsText, '0');
});
test('invalid or incomplete process output cannot restore ready state', () => {
  for (const output of ['', '{}', 'null', '[]', '{bad', '{"status":{}}', '{"connectionState":"ready","status":{"speed":"fast"}}', '{"connectionState":"bogus","status":{"speed":0}}']) {
    assert.throws(() => model.parseSnapshot(output));
  }
  assert.equal(model.parseSnapshot(JSON.stringify(walking)).sessionSteps, 3456);
});

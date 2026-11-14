'use strict';
const assert = require('node:assert/strict');
const {test} = require('node:test');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const readline = require('node:readline');
const {spawn} = require('node:child_process');
const {heldProof, TOOLS, ATTEMPT_MARKER} =
  require('../../resources/runtime/toolhost.cjs');

const TOOLHOST = path.join(__dirname, '../../resources/runtime/toolhost.cjs');

function heldWorkspace(changes = {}) {
  const support = fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-toolhost-'));
  const workspace = fs.mkdtempSync(path.join(support, 'run-'));
  const session = path.basename(workspace);
  const result = {verified: true, fs: {verified: true, restored: true,
    retainedEndpoint: {ready: true, session}, cleanup: {deferred: true, donor: {cleanedUp: true}}},
    rpc: {mode: 'held', state: 'listening'}, ...changes.result};
  fs.writeFileSync(path.join(workspace, 'result.json'), JSON.stringify(result), {mode: changes.mode ?? 0o600});
  fs.writeFileSync(path.join(workspace, 'connection.json'),
    JSON.stringify({port: 39999, addresses: {session: changes.session ?? session, target: 'switch2'}}), {mode: 0o600});
  return workspace;
}

test('only a complete private held proof is accepted', async () => {
  const workspace = heldWorkspace();
  const proof = await heldProof(workspace);
  assert.equal(proof.session, path.basename(workspace));
  assert.equal(proof.connection.port, 39999);
});

test('a finished, public or foreign session is refused', async () => {
  await assert.rejects(heldProof(heldWorkspace({result: {completed: '2026-01-01T00:00:00Z'}})),
    /verified session with a live retained RPC lease/);
  await assert.rejects(heldProof(heldWorkspace({mode: 0o644})), /owned private file/);
  await assert.rejects(heldProof(heldWorkspace({session: 'another-run'})),
    /retained endpoint does not belong to this session/);
  await assert.rejects(heldProof(fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-empty-'))), /ENOENT/);
});

test('the tool catalog adds no write path beyond the modloader', () => {
  assert.deepEqual(TOOLS, ['rpc-status', 'system-code-dump', 'ouncecake-webdav', 'ouncecake-ftp',
    'mintbrew', 'native-hblauncher', 'custom-homebrew', 'modloader']);
});

// ---------- homebrew loopback fixtures ----------
// A fake engine bridge answers the documented commands; fake staged vendor
// trees reproduce the upstream readiness lines, shutdown proofs and session
// files the supervisors verify. No console is involved.

function writeScript(file, body) {
  fs.mkdirSync(path.dirname(file), {recursive: true});
  fs.writeFileSync(file, body, {mode: 0o600});
}

function fakeBridge(t) {
  const commands = [];
  const server = http.createServer((req, res) => {
    const parts = [];
    req.on('data', part => parts.push(part));
    req.on('end', () => {
      const body = JSON.parse(Buffer.concat(parts).toString() || '{}');
      commands.push(body.command);
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify(body.command === 'status' ? {ready: true} : {ok: true}));
    });
  });
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      t.after(() => new Promise(done => { server.closeAllConnections(); server.close(done); }));
      resolve({port: server.address().port, commands});
    });
  });
}

function heldWorkspaceAt(support, port) {
  const workspace = fs.mkdtempSync(path.join(support, 'run-'));
  const session = path.basename(workspace);
  fs.writeFileSync(path.join(workspace, 'result.json'), JSON.stringify({verified: true,
    fs: {verified: true, restored: true, retainedEndpoint: {ready: true, session},
      cleanup: {deferred: true, donor: {cleanedUp: true}}},
    rpc: {mode: 'held', state: 'listening'}}), {mode: 0o600});
  fs.writeFileSync(path.join(workspace, 'connection.json'),
    JSON.stringify({port, addresses: {session, target: 'switch2'}}), {mode: 0o600});
  return workspace;
}

function homebrewRuntimeFixture(support) {
  const root = path.join(support, 'homebrew-runtime');
  for (const file of ['python/bin/python3', 'bin/spirv-val']) {
    fs.mkdirSync(path.dirname(path.join(root, file)), {recursive: true});
    fs.writeFileSync(path.join(root, file), '', {mode: 0o700});
  }
  fs.mkdirSync(path.join(root, 'fonts'), {recursive: true});
  fs.writeFileSync(path.join(root, 'fonts/DejaVuSans.ttf'), 'font', {mode: 0o600});
  fs.mkdirSync(path.join(root, 'python-custom'), {recursive: true});
  fs.writeFileSync(path.join(root, 'python-custom/sitecustomize.py'), '# adapter', {mode: 0o600});
  return root;
}

function stagedRootFixture(support) {
  const stagedRoot = path.join(support, 'staged');
  fs.mkdirSync(stagedRoot, {recursive: true});
  fs.writeFileSync(path.join(stagedRoot, 'stage.json'),
    '{"source":"developer-staged","redistributable":false}', {mode: 0o600});
  return stagedRoot;
}

// A stage script exits 0 unless FAKE_FAIL_STAGE names it.
const stageScript = name => `'use strict';
process.exit(process.env.FAKE_FAIL_STAGE === ${JSON.stringify(name)} ? 1 : 0);
`;

// An owner prints its readiness (and frame) lines, then stays up until
// SIGTERM. onStop is a JS source snippet run in the SIGTERM handler.
const ownerScript = (readyLine, frameLine, onStop = '') => `'use strict';
const fs = require('node:fs');
const path = require('node:path');
const sessionDir = process.argv[2];
const session = path.basename(sessionDir);
console.log(${JSON.stringify(readyLine)});
${frameLine ? 'console.log(' + JSON.stringify(frameLine) + ');' : ''}
process.on('SIGTERM', () => {
  ${onStop}
  process.exit(0);
});
setInterval(() => {}, 1000);
`;

const writeProof = (file, payload) =>
  'fs.writeFileSync(path.join(sessionDir, ' + JSON.stringify(file) + '), JSON.stringify(' + payload + '), {mode: 0o600});';

// The MintBrew launcher stage also probes the loopback proxy and the session
// configuration files exactly the way the pinned launcher does.
const mintbrewProbe = `'use strict';
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const sessionDir = process.argv[2];
const config = JSON.parse(fs.readFileSync(path.join(sessionDir, 'connection.json')));
const post = (body, token) => new Promise((resolve, reject) => {
  const headers = {'content-type': 'application/json'};
  if (token) headers.authorization = 'Bearer ' + token;
  const request = http.request({host: '127.0.0.1', port: config.port, path: '/', method: 'POST', headers},
    response => {
      const parts = [];
      response.on('data', part => parts.push(part));
      response.on('end', () => resolve({status: response.statusCode, body: Buffer.concat(parts).toString()}));
    });
  request.on('error', reject);
  request.end(JSON.stringify(body));
});
(async () => {
  const stremio = JSON.parse(fs.readFileSync(path.join(sessionDir, 'stremio-config.json')));
  if (stremio.quality !== '720p' || stremio.addonUrls.length !== 0 || stremio.libraryItems.length !== 0) {
    throw Error('the MintBrew Stremio configuration changed');
  }
  const host = JSON.parse(fs.readFileSync(path.join(sessionDir, 'mintbrew-host.json')));
  if (typeof host.runDirectory !== 'string' || typeof host.sourceSession !== 'string') {
    throw Error('the MintBrew host configuration is incomplete');
  }
  const ok = await post({command: 'status'}, config.token);
  if (ok.status !== 200 || JSON.parse(ok.body).ready !== true) throw Error('the RPC proxy refused status');
  const unknown = await post({command: 'format-c-card'}, config.token);
  if (unknown.status !== 400) throw Error('the RPC proxy allowed an unknown command');
  const anonymous = await post({command: 'status'});
  if (anonymous.status !== 403) throw Error('the RPC proxy answered without its token');
})().then(() => process.exit(0), error => { console.error(error.message); process.exit(1); });
`;

function mintbrewStage(stagedRoot) {
  const vendor = path.join(stagedRoot, 'tools/mintbrew/vendor/MintBrew');
  fs.mkdirSync(path.join(vendor, 'apps'), {recursive: true});
  fs.writeFileSync(path.join(vendor, 'manifest.json'),
    JSON.stringify({name: 'MintBrew', version: '1.2.3'}), {mode: 0o600});
  writeScript(path.join(vendor, 'smilebasic/engine/shared/rpc.cjs'), '// frozen rpc client\n');
  for (const name of ['inspect-display', 'inspect-compatibility', 'inspect-input', 'vulkan-copy', 'launcher-check']) {
    writeScript(path.join(vendor, 'homebrew-launcher', name + '.cjs'), stageScript('homebrew-launcher/' + name + '.cjs'));
  }
  writeScript(path.join(vendor, 'homebrew-launcher/launcher.cjs'), mintbrewProbe);
  writeScript(path.join(vendor, 'homebrew-launcher/launcher-loop.cjs'),
    ownerScript('MintBrew game event source ready.', 'MintBrew continuous native frame loop running.',
      "if (!process.env.FAKE_UNPROVEN) { " + writeProof('launcher-auto-loop.json',
        '{session, completed: true, threadRunning: false}') + ' }'));
  writeScript(path.join(vendor, 'homebrew-launcher/game-audio.cjs'),
    ownerScript('Game sound effects ready.', '',
      writeProof('game-audio.json', '{session, completed: true}')));
  writeScript(path.join(vendor, 'sdk/host/ftp-service.cjs'), ownerScript('SDK FTP service ready.'));
}

async function mintbrewWorld(t) {
  const support = fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-hb-'));
  t.after(() => fs.rmSync(support, {recursive: true, force: true}));
  const bridge = await fakeBridge(t);
  const workspace = heldWorkspaceAt(support, bridge.port);
  const stagedRoot = stagedRootFixture(support);
  mintbrewStage(stagedRoot);
  const config = {supportDir: support, homebrewRuntime: homebrewRuntimeFixture(support)};
  return {support, bridge, workspace, stagedRoot, config};
}

function runToolhost(args, {env = {}, stop = true} = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [TOOLHOST, ...args],
      {env: {...process.env, ...env}, stdio: ['pipe', 'pipe', 'pipe']});
    const events = [];
    let stderr = '';
    child.stderr.on('data', part => { stderr += part; });
    readline.createInterface({input: child.stdout}).on('line', line => {
      const event = JSON.parse(line);
      events.push(event);
      if (event.type === 'running' && stop) child.stdin.write('{"type":"stop"}\n');
    });
    child.once('error', reject);
    child.once('close', (code, signal) => resolve({events, code, signal, stderr}));
  });
}

const markerPath = workspace => path.join(workspace, ATTEMPT_MARKER);

test('mintbrew proves its cleanup and releases the attempt marker', {timeout: 120000}, async t => {
  const {bridge, workspace, stagedRoot, config} = await mintbrewWorld(t);
  const {events, code, stderr} = await runToolhost(
    [stagedRoot, 'mintbrew', workspace, '-', '-', JSON.stringify(config)]);
  assert.equal(code, 0, stderr + '\n' + JSON.stringify(events));
  for (const type of ['started', 'running']) assert.ok(events.some(e => e.type === type), type);
  const stopped = events.find(e => e.type === 'stopped');
  assert.ok(stopped, JSON.stringify(events));
  assert.equal(stopped.cleanupVerified, true);
  assert.match(stopped.attemptMarkerSha256, /^[a-f0-9]{64}$/);
  assert.deepEqual(stopped.attemptMarkers, [ATTEMPT_MARKER]);
  assert.equal(fs.existsSync(markerPath(workspace)), false, 'the marker must be released');
  assert.ok(bridge.commands.includes('status'), 'the proxy forwarded status to the bridge');
});

test('mintbrew refuses a second owner of the console session', {timeout: 60000}, async t => {
  const {workspace, stagedRoot, config} = await mintbrewWorld(t);
  fs.writeFileSync(markerPath(workspace), '{"application":"mintbrew"}\n', {mode: 0o600});
  const {events, code} = await runToolhost(
    [stagedRoot, 'mintbrew', workspace, '-', '-', JSON.stringify(config)], {stop: false});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.ok(failed, JSON.stringify(events));
  assert.match(failed.message, /already owns this console session/);
  assert.ok(fs.existsSync(markerPath(workspace)), 'the foreign marker must stay untouched');
});

test('a mintbrew failure before any console change stays retryable', {timeout: 120000}, async t => {
  const {workspace, stagedRoot, config} = await mintbrewWorld(t);
  const {events, code} = await runToolhost(
    [stagedRoot, 'mintbrew', workspace, '-', '-', JSON.stringify(config)],
    {stop: false, env: {FAKE_FAIL_STAGE: 'homebrew-launcher/inspect-display.cjs'}});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.ok(failed, JSON.stringify(events));
  assert.match(failed.message, /preparation failed/);
  assert.match(failed.message, /still available; choose another app or retry/);
  assert.equal(failed.cleanupBlocked, false);
  assert.equal(fs.existsSync(markerPath(workspace)), false, 'a retry-safe failure releases the marker');
});

test('an unproven mintbrew shutdown keeps the marker and blocks re-entry', {timeout: 120000}, async t => {
  const {workspace, stagedRoot, config} = await mintbrewWorld(t);
  const {events, code} = await runToolhost(
    [stagedRoot, 'mintbrew', workspace, '-', '-', JSON.stringify(config)],
    {env: {FAKE_UNPROVEN: '1'}});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.ok(failed, JSON.stringify(events));
  assert.match(failed.message, /did not verify its graphics and audio shutdown/);
  assert.match(failed.message, /Restart the console before launching homebrew again/);
  assert.equal(failed.cleanupBlocked, true);
  assert.ok(fs.existsSync(markerPath(workspace)), 'an unproven cleanup keeps the marker');
});

test('heldProof accepts verified civ7 and artemis-seedsow sessions', async t => {
  const support = fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-targets-'));
  t.after(() => fs.rmSync(support, {recursive: true, force: true}));
  for (const [prefix, target] of [['civswitch-fs-0123456789abcdef', 'civ7'], ['artemis-fs-0123456789abcdef', 'artemis-seedsow']]) {
    const workspace = path.join(support, 'runs', prefix, 'port/engine/runs/switch2-abc123');
    fs.mkdirSync(workspace, {recursive: true, mode: 0o700});
    const session = path.basename(workspace);
    fs.writeFileSync(path.join(workspace, 'result.json'), JSON.stringify({
      target, verified: true,
      fs: {verified: true, restored: true, retainedEndpoint: {ready: true, session}, cleanup: {deferred: true, donor: {cleanedUp: true}}},
      rpc: {mode: 'held', state: 'listening'},
    }), {mode: 0o600});
    fs.writeFileSync(path.join(workspace, 'connection.json'),
      JSON.stringify({port: 39999, addresses: {session, target}}), {mode: 0o600});
    const proof = await heldProof(workspace);
    assert.equal(proof.target, target);
    assert.equal(proof.session, session);
  }
});

test('native hblauncher rejects an untrusted profile before loading the host', {timeout: 60000}, async t => {
  const support = fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-native-'));
  t.after(() => fs.rmSync(support, {recursive: true, force: true}));
  const bridge = await fakeBridge(t);
  const workspace = heldWorkspaceAt(support, bridge.port);
  const stagedRoot = stagedRootFixture(support);
  const config = {supportDir: support, profileID: 'bogus'};
  const {events, code} = await runToolhost(
    [stagedRoot, 'native-hblauncher', workspace, '-', '-', JSON.stringify(config)], {stop: false});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.ok(failed, JSON.stringify(events));
  assert.match(failed.message, /supported held RPC host profile/);
  assert.equal(failed.cleanupBlocked, false);
});

// ---------- modloader loopback fixtures ----------
// A fake staged ns2jb-modloader: its collector accepts contents/<id> folders
// and its installer only writes the handoff proofs a real install reports.
const fakeInstaller = `'use strict';
const fs = require('node:fs');
const path = require('node:path');
const run = process.argv[process.argv.indexOf('--run-dir') + 1];
if (process.env.FAKE_MOD_FAIL) { console.log('INSTALL STOPPED: fixture refused.'); process.exit(1); }
const state = path.join(__dirname, 'state', path.basename(run));
fs.mkdirSync(state, {recursive: true});
fs.writeFileSync(path.join(state, 'resident-install.json'), JSON.stringify({verified: true,
  readyForGameHandoff: true, handoff: {stockRoProtocolVerified: true}}));
fs.writeFileSync(path.join(run, 'ns2jb-modloader-install.json'), JSON.stringify({status: 'ready'}));
console.log('READY');
`;
const fakeCollector = `'use strict';
const fs = require('node:fs');
const path = require('node:path');
exports.collectMods = root => {
  const contents = path.join(root, 'contents');
  const games = fs.existsSync(contents) ? fs.readdirSync(contents).map(titleId => ({titleId})) : [];
  if (!games.length) throw Error('No mods found.');
  return {games, patches: []};
};
`;

async function modloaderWorld(t) {
  const support = fs.mkdtempSync(path.join(os.tmpdir(), 'any2jb-mod-'));
  t.after(() => fs.rmSync(support, {recursive: true, force: true}));
  const bridge = await fakeBridge(t);
  const workspace = heldWorkspaceAt(support, bridge.port);
  const stagedRoot = stagedRootFixture(support);
  const component = path.join(stagedRoot, 'tools/modloader/integrations/ns2jb-modloader');
  writeScript(path.join(component, 'install.cjs'), fakeInstaller);
  writeScript(path.join(component, 'LICENSE'), 'GPL v2 fixture\n');
  writeScript(path.join(component, 'lib/mods.cjs'), fakeCollector);
  writeScript(path.join(component, 'lib/native/artifacts.cjs'), 'exports.artifacts = () => ({});\n');
  writeScript(path.join(component, 'native/prebuilt/manifest.json'), '{}\n');
  const mods = path.join(support, 'mods');
  writeScript(path.join(mods, 'contents/0100000000010000/romfs/data.bin'), 'mod');
  const config = {supportDir: support, profileID: 'kurumuz', modsFolder: mods, codeConsent: true};
  return {bridge, workspace, stagedRoot, config, mods};
}

const modloaderStaging = workspace => fs.readdirSync(workspace).filter(name => name.startsWith('.any2jb-modloader-'));

test('the modloader refuses missing code consent before staging or console requests', async t => {
  const {bridge, workspace, stagedRoot, config} = await modloaderWorld(t);
  const {events, code} = await runToolhost(
    [stagedRoot, 'modloader', workspace, '-', '-', JSON.stringify({...config, codeConsent: false})], {stop: false});
  assert.equal(code, 1);
  assert.match(events.find(e => e.type === 'failed').message, /Confirm/);
  assert.deepEqual(modloaderStaging(workspace), []);
  assert.deepEqual(bridge.commands, []);
});

test('user homebrew needs boolean consent before any console request', async t => {
  const {bridge, workspace, stagedRoot, config, mods} = await modloaderWorld(t);
  for (const selected of ['custom-homebrew', 'native-hblauncher']) {
    for (const codeConsent of [undefined, false, 'true']) {
      const launch = {...config, appFolder: mods, customHomebrewFolder: mods, codeConsent};
      const {events, code} = await runToolhost(
        [stagedRoot, selected, workspace, '-', '-', JSON.stringify(launch)], {stop: false});
      assert.equal(code, 1);
      assert.match(events.find(e => e.type === 'failed').message, /Confirm/);
    }
  }
  assert.deepEqual(bridge.commands, []);
  assert.equal(fs.existsSync(markerPath(workspace)), false);
});

test('the modloader verifies its game handoff as the final action', {timeout: 60000}, async t => {
  const {workspace, stagedRoot, config} = await modloaderWorld(t);
  const {events, code, stderr} = await runToolhost(
    [stagedRoot, 'modloader', workspace, '-', '-', JSON.stringify(config)], {stop: false});
  assert.equal(code, 0, stderr + '\n' + JSON.stringify(events));
  const ready = events.find(e => e.type === 'ready');
  assert.ok(ready, JSON.stringify(events));
  assert.match(ready.message, /overlay stays until the console restarts/);
});

test('an invalid mod folder is refused before any transfer', {timeout: 60000}, async t => {
  const {workspace, stagedRoot, config, mods} = await modloaderWorld(t);
  fs.symlinkSync('/etc', path.join(mods, 'contents/0100000000010000/romfs/link'));
  const {events, code} = await runToolhost(
    [stagedRoot, 'modloader', workspace, '-', '-', JSON.stringify(config)], {stop: false});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.ok(failed, JSON.stringify(events));
  assert.equal(failed.cleanupBlocked, false);
  assert.match(failed.message, /regular files and directories/);
  assert.deepEqual(modloaderStaging(workspace), []);
  assert.equal(fs.existsSync(path.join(workspace, 'ns2jb-modloader-install.json')), false);
});

test('the modloader refuses a profile without modloader capability', {timeout: 60000}, async t => {
  const {workspace, stagedRoot, config} = await modloaderWorld(t);
  const {events, code} = await runToolhost(
    [stagedRoot, 'modloader', workspace, '-', '-', JSON.stringify({...config, profileID: 'astral'})], {stop: false});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.match(failed.message, /Kurumuz or Cellenseres/);
  assert.equal(failed.cleanupBlocked, false);
});

test('an unverified modloader handoff requires a console restart', {timeout: 60000}, async t => {
  const {workspace, stagedRoot, config} = await modloaderWorld(t);
  const {events, code} = await runToolhost(
    [stagedRoot, 'modloader', workspace, '-', '-', JSON.stringify(config)], {stop: false, env: {FAKE_MOD_FAIL: '1'}});
  assert.equal(code, 1);
  const failed = events.find(e => e.type === 'failed');
  assert.equal(failed.cleanupBlocked, true);
  assert.match(failed.message, /fixture refused.*Restart the console/);
});

test('custom homebrew runs a built GPU app from a fresh session library', {timeout: 120000}, async t => {
  const {workspace, stagedRoot, config, support} = await mintbrewWorld(t);
  const app = path.join(support, 'my-app');
  writeScript(path.join(app, 'app.json'), JSON.stringify({id: 'hello-gpu', name: 'Hello'}));
  const {events, code, stderr} = await runToolhost([stagedRoot, 'custom-homebrew', workspace, '-', '-',
    JSON.stringify({...config, customHomebrewFolder: app, codeConsent: true})]);
  assert.equal(code, 0, stderr + '\n' + JSON.stringify(events));
  assert.ok(events.some(e => e.type === 'stopped' && e.cleanupVerified), JSON.stringify(events));
  assert.deepEqual(fs.readdirSync(app), ['app.json'], 'the original folder stays unchanged');
});

test('custom homebrew refuses an unsupported format before the console is touched', {timeout: 60000}, async t => {
  const {bridge, workspace, stagedRoot, config, support} = await mintbrewWorld(t);
  const module = path.join(support, 'iz-module');
  writeScript(path.join(module, 'module.json'), '{}');
  writeScript(path.join(module, 'app.json'), JSON.stringify({id: 'x', name: 'X'}));
  const {events, code} = await runToolhost([stagedRoot, 'custom-homebrew', workspace, '-', '-',
    JSON.stringify({...config, customHomebrewFolder: module, codeConsent: true})], {stop: false});
  assert.equal(code, 1);
  assert.equal(events.find(e => e.type === 'failed').cleanupBlocked, false);
  assert.equal(fs.existsSync(markerPath(workspace)), false);
  assert.deepEqual(bridge.commands, ['status']);
});

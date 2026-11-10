'use strict';

// Go owns policy, integrity, workspace, process group and verification. This
// adapter only joins upstream connection/native/pipeline APIs to JSON lines.
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const path = require('node:path');
const {spawn} = require('node:child_process');
const readline = require('node:readline');

async function writableCopy(source, destination) {
  await fsp.cp(source, destination, {recursive: true});
  async function unseal(directory) {
    await fsp.chmod(directory, 0o700);
    for (const entry of await fsp.readdir(directory, {withFileTypes: true})) {
      const name = path.join(directory, entry.name);
      if (entry.isDirectory()) await unseal(name);
      else if (entry.isFile()) await fsp.chmod(name, 0o600);
      else throw Error('Unexpected copied engine file');
    }
  }
  await unseal(destination);
}

async function launch(manifestPath, profile, workspace, runID, host) {
  const root = path.dirname(manifestPath);
  const record = JSON.parse(await fsp.readFile(path.join(root, 'stage.json'), 'utf8'));
  if (record.source !== 'developer-staged' || record.redistributable !== false ||
      record.profileID !== profile.id || record.revision !== profile.revision ||
      record.sourceSha256 !== profile.sourceSha256)
    throw Error('Engine launch requires a matching developer stage');
  if (profile.engine === 'izmysh') throw Error('IZMYSH requires its separate native qualification adapter');
  const source = path.join(root, profile.sourceDirectory);
  const {listen, pairingCode} = require(path.join(source, 'engine/lib/connection.cjs'));
  const {NativeClient} = require(path.join(source, 'engine/lib/native.cjs'));
  const {qualify} = require(path.join(source, 'engine/lib/qualify.cjs'));
  const {serve} = require(path.join(source, 'engine/lib/bridge.cjs'));
  const save = (name, data) => fsp.writeFile(path.join(workspace, name), JSON.stringify(data) + '\n', {mode: 0o600});
  let sequence = 0;
  const emit = (type, fields = {}) => process.stdout.write(JSON.stringify({type, sequence: ++sequence, runID, ...fields}) + '\n');
  // pepsipu uses fresh pairing tokens; the five-argument family uses none.
  const token = profile.startArgumentCount === 9 ? pairingCode() : undefined;
  const command = `V=START(${[profile.startPort, ...host.split('.'), ...(token || [])].join(',')})`;
  let native, bridge, child, finished, heldReady = false, failure, ownerGone = false;
  const cleanupOwner = () => {
    const support = path.dirname(path.dirname(workspace));
    const lease = path.join(support, 'session.lock');
    const info = fs.lstatSync(lease);
    if (path.basename(path.dirname(workspace)) !== 'runs' || !path.basename(workspace).startsWith('run-') ||
        !info.isFile() || info.uid !== process.getuid() || (info.mode & 0o077) ||
        fs.readFileSync(lease, 'utf8') !== runID) throw Error('Session lease ownership changed');
    const past = path.join(support, 'past-runs', path.basename(workspace));
    for (const name of ['backups', 'savekit-exports', 'verified-exports', 'dumps',
      'host.log', 'run.log', 'usb.log', 'diagnostics-id', 'switch2/acquisition.log', 'switch2/pipeline.json']) {
      const source = path.join(workspace, name);
      const entry = fs.lstatSync(source, {throwIfNoEntry: false});
      if (!entry) continue;
      if (entry.isSymbolicLink() || (!entry.isFile() && !entry.isDirectory()))
        throw Error('Invalid retained session artifact');
      const destination = path.join(past, name);
      fs.mkdirSync(path.dirname(destination), {recursive: true, mode: 0o700});
      if (fs.existsSync(destination)) throw Error('Retained artifact destination exists');
      fs.renameSync(source, destination);
    }
    fs.writeFileSync(path.join(support, 'session-interrupted'), runID, {mode: 0o600});
    fs.rmSync(workspace, {recursive: true});
    fs.unlinkSync(lease);
  };
  const ownerExit = () => {
    try { cleanupOwner(); }
    finally {
      // This bridge is the group leader. The Go owner has died and cannot
      // deliver the final group kill after graceful child cleanup.
      process.kill(-process.pid, 'SIGKILL');
    }
  };
  const result = {verified: false, started: new Date().toISOString()};
  let requestFinish;
  const finish = new Promise(resolve => { requestFinish = resolve; });
  const lines = readline.createInterface({input: process.stdin});
  process.stdin.once('end', () => {
    ownerGone = true;
    setTimeout(ownerExit, profile.supportsHeldRPC ? 300000 : 2000);
    if (!native) ownerExit();
    else if (heldReady) requestFinish('owner-exited');
    else {
      native.connection.close();
      child?.kill('SIGTERM');
    }
  });
  lines.on('line', line => {
    if (line !== '{"type":"finish"}' || !heldReady) {
      failure ||= Error('Out-of-order bridge input');
      if (!heldReady) process.exit(1);
    }
    requestFinish('finish');
  });
  const stop = () => {
    if (!heldReady) process.exit(143);
    requestFinish('stop');
  };
  process.on('SIGTERM', stop);
  process.on('SIGINT', stop);
  const check = outcome => {
    if (outcome.error || outcome.code !== 0 || outcome.signal) throw Error('Engine child did not exit cleanly');
  };
  try {
    const connected = await listen({host, port: profile.startPort, ...(token ? {token} : {}),
      onWaiting() { emit('ready', {command}); }});
    emit('connected');
    native = new NativeClient(connected.connection, connected.addresses);
    result.bootstrap = await qualify(native);
    bridge = await serve(native);
    bridge.keepAwake(result.bootstrap.activity);
    await save('connection.json', {port: bridge.port, ...(bridge.token ? {token: bridge.token} : {}),
      addresses: {...connected.addresses, session: path.basename(workspace), target: 'switch2'}});
    // Never invoke upstream run.cjs main: it writes beside its own sealed source.
    // Only the per-run pipeline copies are writable.
    for (const name of ['shared', 'switch2'])
      await writableCopy(path.join(source, 'engine', name), path.join(workspace, name));
    if (ownerGone) throw Error('Host owner exited');
    const log = fs.createWriteStream(path.join(workspace, 'run.log'), {flags: 'wx', mode: 0o600});
    const held = profile.supportsHeldRPC === true;
    child = spawn(process.execPath, [path.join(workspace, 'switch2/pipeline.cjs'), ...(held ? ['--hold-rpc'] : [])],
      {cwd: workspace, stdio: ['ignore', 'pipe', 'pipe', ...(held ? ['ipc'] : [])], windowsHide: true});
    child.stdout.pipe(log, {end: false});
    child.stderr.pipe(log, {end: false});
    readline.createInterface({input: child.stdout}).on('line', line => {
      const stage = /^([1-4])\/4 /.exec(line);
      if (stage) emit('stage', {stage: Number(stage[1])});
    });
    finished = new Promise(resolve => {
      child.once('error', error => resolve({error}));
      child.once('close', (code, signal) => resolve({code, signal}));
    }).then(async outcome => {
      await new Promise((resolve, reject) => { log.once('error', reject); log.end(resolve); });
      return outcome;
    });
    log.on('error', error => { failure ||= error; child.kill('SIGTERM'); });
    if (held) {
      await Promise.race([
        new Promise(resolve => child.on('message', value => { if (value?.type === 'held-ready') resolve(); })),
        finished.then(() => { throw Error('Engine exited before held-ready'); }),
      ]);
    } else check(await finished);
    result.fs = JSON.parse(await fsp.readFile(path.join(workspace, 'switch2/fs-execution-current.json'), 'utf8'));
    result.verified = true;
    if (held) {
      result.rpc = {mode: 'held', state: 'listening'};
      await save('result.json', result);
      heldReady = true;
      emit('held');
      const reason = await Promise.race([
        finish,
        finished.then(() => { throw Error('Held endpoint exited unexpectedly'); }),
        new Promise(resolve => native.connection.socket.once('close', () => resolve('disconnected'))),
      ]);
      if (child.connected) child.send({type: 'release'});
      check(await finished);
      result.fs = JSON.parse(await fsp.readFile(path.join(workspace, 'switch2/fs-execution-current.json'), 'utf8'));
      result.rpc.state = 'closed';
      if (reason !== 'finish') throw Error('Held session stopped without Finish');
    }
  } catch (error) {
    failure ||= error;
    result.error = 'Engine operation failed; restart the console after any side effect.';
  } finally {
    if (child?.connected) {
      child.send({type: 'release'});
      try { check(await finished); }
      catch (error) { failure ||= error; (result.cleanupErrors ||= []).push({name: 'pipeline'}); }
    }
    for (const [name, close] of [['bridge', () => bridge?.close()], ['connection', () => native?.stop()]]) {
      try { await close(); }
      catch (error) { failure ||= error; (result.cleanupErrors ||= []).push({name}); }
    }
    result.completed = new Date().toISOString();
    await save('result.json', result);
    lines.close();
    process.stdin.pause();
    process.removeListener('SIGTERM', stop);
    process.removeListener('SIGINT', stop);
  }
  if (ownerGone) ownerExit();
  emit(failure ? 'failed' : 'result');
  if (failure) process.exitCode = 1;
}

async function main(args = process.argv.slice(2)) {
  const [manifestPath, profileID, mode, workspace, runID, host] = args;
  if (!manifestPath || !profileID || ![2, 6].includes(args.length))
    throw Error('usage: bridge.cjs <canonical-manifest> <profile-id> [--launch <workspace> <run-id> <IPv4>]');
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  const profile = manifest.profiles.find(profile => profile.id === profileID);
  if (!profile) throw new Error('unknown host profile');
  if (args.length === 6) {
    if (mode !== '--launch' || !require('node:net').isIPv4(host) || !runID ||
        fs.realpathSync(workspace) !== fs.realpathSync(process.cwd())) throw Error('Invalid launch request');
    return launch(manifestPath, profile, workspace, runID, host);
  }
  if (profile.distribution.decision !== 'may-ship' || !profile.distribution.bundled) {
    throw new Error(profile.distribution.reason);
  }
  process.stdout.write(JSON.stringify({type: 'profile', profile}) + '\n');
}

if (require.main === module) main().catch(error => {
  process.stderr.write(JSON.stringify({type: 'error', message: error.message}) + '\n');
  process.exitCode = 1;
});
module.exports = {main};

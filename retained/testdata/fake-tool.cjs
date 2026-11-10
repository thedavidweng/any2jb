'use strict';
// Structured-process fixture only. No console, exploit or upstream tool is
// emulated, and no console filesystem is touched.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const readline = require('node:readline');
const [, tool, workspace, consoleAddress, destination, scenario = 'ok'] = process.argv.slice(2);
let sequence = 0;
const emit = (type, fields = {}) =>
  process.stdout.write(JSON.stringify({type, sequence: ++sequence, tool, ...fields}) + '\n');

const lines = readline.createInterface({input: process.stdin});
let resolveStop, resolveSelection;
const stopped = new Promise(resolve => { resolveStop = resolve; });
const selected = new Promise(resolve => { resolveSelection = resolve; });
lines.on('line', line => {
  let command;
  try { command = JSON.parse(line); } catch { return; }
  if (command.type === 'stop') resolveStop();
  else if (command.type === 'select') resolveSelection(command.id);
});
lines.on('close', () => resolveStop());
process.on('SIGTERM', () => resolveStop());

async function main() {
  if (!fs.existsSync(path.join(workspace, 'result.json'))) throw Error('fixture requires a held run');
  if (tool === 'rpc-status') { emit('status', {ready: true}); return; }
  emit('started');
  if (scenario === 'dirty') { process.exitCode = 1; return; }
  if (tool === 'system-code-dump') {
    fs.mkdirSync(destination, {recursive: true, mode: 0o700});
    fs.writeFileSync(path.join(destination, 'firmware-dump.json'),
      JSON.stringify({complete: true, inventory: {total: 1}, entries: [{status: 'codefs-unavailable'}]}), {mode: 0o600});
    emit('complete', {path: scenario === 'outside' ? workspace : destination, total: 1});
    return;
  }
  if (tool === 'ouncecake-webdav' || tool === 'ouncecake-ftp') {
    emit('catalog', {games: [{titleId: '0123456789abcdef', name: 'Fixture Game'}]});
    const title = await selected;
    if (title !== '0123456789abcdef') throw Error('unexpected fixture selection');
    emit('running', {readOnly: true, endpoint: tool === 'ouncecake-ftp'
      ? 'ftp://127.0.0.1:2222/' : 'http://' + consoleAddress + ':2121/'});
  } else if (tool === 'sd-browser') {
    emit('running', {readOnly: true, endpoint: scenario === 'exposed'
      ? 'ftp://192.0.2.9:2122/sd/' : 'ftp://' + consoleAddress + ':2122/sd/'});
  } else if (tool === 'modloader') {
    if (scenario === 'mod-fail-retry' || scenario === 'mod-fail-blocked') {
      emit('failed', {cleanupBlocked: scenario === 'mod-fail-blocked', message: 'fixture modloader failure'});
      process.exitCode = 1;
      return;
    }
    emit('progress', {message: 'READY'});
    emit('ready', {message: 'fixture handoff verified'});
    return;
  } else if (['mintbrew', 'native-hblauncher', 'izlauncher', 'custom-homebrew'].includes(tool)) {
    const marker = path.join(workspace, '.mac2jb-hblauncher-attempt.json');
    fs.writeFileSync(marker, JSON.stringify({application: tool, started: 'fixture'}) + '\n',
      {mode: 0o600, flag: 'wx'});
    if (scenario === 'hb-fail-blocked' || scenario === 'hb-fail-retry') {
      if (scenario === 'hb-fail-retry') fs.unlinkSync(marker);
      emit('failed', {cleanupBlocked: scenario === 'hb-fail-blocked',
        message: 'fixture launch failure'});
      process.exitCode = 1;
      return;
    }
    emit('running');
    await stopped;
    if (scenario === 'hb-unproven') { emit('stopped'); return; }
    const digest = crypto.createHash('sha256').update(fs.readFileSync(marker)).digest('hex');
    fs.unlinkSync(marker);
    emit('stopped', {cleanupVerified: true, attemptMarkerSha256: digest,
      attemptMarkers: ['.mac2jb-hblauncher-attempt.json']});
    return;
  } else throw Error('unknown fixture tool');
  if (scenario === 'progress-flood') {
    for (let i = 0; i < 64; i++) emit('progress');
  }
  await stopped;
  emit('stopped');
}

main()
  .catch(error => { emit('failed', {message: error.message}); process.exitCode = 1; })
  .finally(() => { lines.close(); process.stdin.pause(); });

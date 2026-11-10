'use strict';

// Go owns process/session orchestration. This bridge only exposes the selected
// canonical profile as a structured message; it does not wrap mac2jb's GUI.
const fs = require('node:fs');
const [manifestPath, profileID] = process.argv.slice(2);
try {
  if (!manifestPath || !profileID || process.argv.length !== 4) {
    throw new Error('usage: bridge.cjs <canonical-manifest> <profile-id>');
  }
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  const profile = manifest.profiles.find(profile => profile.id === profileID);
  if (!profile) throw new Error('unknown host profile');
  if (profile.distribution.decision !== 'may-ship' || !profile.distribution.bundled) {
    throw new Error(profile.distribution.reason);
  }
  process.stdout.write(JSON.stringify({type: 'profile', profile}) + '\n');
} catch (error) {
  process.stderr.write(JSON.stringify({type: 'error', message: error.message}) + '\n');
  process.exitCode = 1;
}

'use strict';
const fs = require('node:fs');
const path = require('node:path');
const [flag, root] = process.argv.slice(2);
const marker = path.join(root, 'homebrew', 'IZ2S-Platform', 'mac2jb-runtime.json');
if (flag === '--check') process.exit(fs.existsSync(marker) ? 0 : 1);
if (flag === '--prepare') {
  const counter = path.join(root, 'prepare-count');
  const n = (fs.existsSync(counter) ? parseInt(fs.readFileSync(counter, 'utf8'), 10) : 0) + 1;
  fs.writeFileSync(counter, String(n));
  fs.mkdirSync(path.dirname(marker), {recursive: true});
  fs.writeFileSync(marker, '{"version":2}\n', {flag: 'wx'});
  process.exit(0);
}
process.exit(2);

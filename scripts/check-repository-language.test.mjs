import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import test from 'node:test';
import { validateRepositoryLanguage } from './check-repository-language.mjs';

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'dancehub-language-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  execFileSync('git', ['init', '--quiet'], { cwd: root });
  return root;
}

test('checks hidden, untracked and ignored-but-tracked files; skips deleted files and local caches', (t) => {
  const root = fixture(t);
  const han = String.fromCodePoint(0x4e2d);
  fs.writeFileSync(path.join(root, '.gitignore'), 'local/\ntracked.txt\n');
  fs.mkdirSync(path.join(root, 'local'));
  fs.writeFileSync(path.join(root, 'local', 'notes.txt'), han);
  fs.writeFileSync(path.join(root, 'tracked.txt'), han);
  fs.writeFileSync(path.join(root, 'deleted.txt'), han);
  execFileSync('git', ['add', '--force', 'tracked.txt', 'deleted.txt'], { cwd: root });
  fs.unlinkSync(path.join(root, 'deleted.txt'));
  fs.writeFileSync(path.join(root, '.hidden'), `English\n${han}`);
  fs.writeFileSync(path.join(root, 'new.txt'), han);
  const result = validateRepositoryLanguage(root);
  assert.equal(result.errors.length, 3);
  for (const file of ['tracked.txt:1', '.hidden:2', 'new.txt:1'])
    assert.ok(
      result.errors.some((error) => error.startsWith(file)),
      file,
    );
});

test('rejects encoded text, supplementary Han, filenames and UTF-16 while allowing English symbols', (t) => {
  const root = fixture(t);
  const point = 0x4e2d;
  const han = String.fromCodePoint(point);
  const supplementary = String.fromCodePoint(0x20000);
  const encoded = [
    '\\' + 'u' + point.toString(16),
    '\\' + 'u{' + point.toString(16) + '}',
    '\\' + 'U' + point.toString(16).padStart(8, '0'),
    '&#x' + point.toString(16) + ';',
    '&#' + point + ';',
    encodeURIComponent(han),
    JSON.stringify(supplementary),
    '\\' +
      'u' +
      supplementary.charCodeAt(0).toString(16) +
      '\\' +
      'u' +
      supplementary.charCodeAt(1).toString(16),
  ];
  fs.writeFileSync(path.join(root, 'encoded.txt'), encoded.join('\n'));
  fs.writeFileSync(path.join(root, 'wide.txt'), Buffer.from('\ufeff' + han, 'utf16le'));
  fs.writeFileSync(path.join(root, han + '.txt'), 'English');
  fs.writeFileSync(path.join(root, 'english.md'), 'English: cafe, 42%, arrows → and symbols ×.');
  fs.writeFileSync(path.join(root, 'image.bin'), Buffer.from([0, 255, 1, 2]));
  const result = validateRepositoryLanguage(root);
  assert.equal(result.errors.length, encoded.length + 2);
  assert.equal(result.binaries, 1);
  assert.ok(result.errors.some((error) => error.includes('characters in path')));
  assert.ok(result.errors.some((error) => error.startsWith('wide.txt:1')));
  assert.ok(!result.errors.some((error) => error.startsWith('english.md')));
});

import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {
  markdownReferences,
  markdownAnchors,
  validateMarkdownFiles,
} from './check-markdown-links.mjs';

test('Markdown references cover images, nested image links, reference definitions and HTML', () => {
  const source = [
    '[![preview](./image.png)](./README.md)',
    '[guide][manual] ![asset][picture]',
    '[manual]: <./a file.md> "Guide"',
    '[picture]: ./asset%20one.png',
    '[nested](./a(b).md) [escaped](./a\\(b\\).md)',
    '<img src="./html.png"> <a href="./other.md">other</a>',
    '`[example](absent.md)`',
    '<!-- [comment](absent.md) -->',
    '```md',
    '[example](absent.md)',
    '```',
  ].join('\n');
  const references = markdownReferences(source);
  assert.ok(references.some((item) => item.image && item.target === './image.png'));
  for (const target of [
    './README.md',
    './a file.md',
    './asset%20one.png',
    './a(b).md',
    './html.png',
    './other.md',
  ])
    assert.ok(
      references.some((item) => item.target === target),
      target,
    );
  assert.ok(!references.some((item) => item.target === 'absent.md'));
});

test('Markdown anchors preserve Unicode and resolve duplicate headings and explicit IDs', () => {
  const anchors = markdownAnchors(
    '# Café Über\n## Hello `Code`!\n## Hello `Code`!\n<a id="custom"></a>\nSetext\n------',
  );
  for (const anchor of ['café-über', 'hello-code', 'hello-code-1', 'custom', 'setext'])
    assert.ok(anchors.has(anchor), anchor);
});

test('local Markdown checks reject deleted images, broken anchors and escaping paths', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'dancehub-markdown-'));
  try {
    await fs.writeFile(
      path.join(root, 'README.md'),
      '# Café Über\n[valid](#café-über)\n![one](./one.png)\n[ref][guide]\n[guide]: <./a file.md#chapter>\n[remote](https://example.invalid/missing)',
    );
    await fs.writeFile(path.join(root, 'one.png'), 'fixture');
    await fs.writeFile(path.join(root, 'a file.md'), '# Chapter');
    assert.deepEqual(validateMarkdownFiles(root, ['README.md', 'already-deleted.md']).errors, []);
    await fs.unlink(path.join(root, 'one.png'));
    await fs.appendFile(
      path.join(root, 'README.md'),
      '\n[bad](./a%20file.md#missing)\n[outside](../outside.md)',
    );
    const result = validateMarkdownFiles(root, ['README.md']);
    assert.equal(result.errors.length, 3);
    assert.ok(result.errors.some((error) => /README.md:3: missing local image/.test(error)));
    assert.ok(result.errors.some((error) => /missing Markdown anchor/.test(error)));
    assert.ok(result.errors.some((error) => /leaves the repository/.test(error)));
  } finally {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith('dancehub-markdown-'));
    await fs.rm(root, { recursive: true, force: true });
  }
});

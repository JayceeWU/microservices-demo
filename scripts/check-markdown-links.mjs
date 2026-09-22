import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const normalize = (label) => label.trim().replace(/\s+/g, ' ').toLowerCase();
const unescape = (value) =>
  value.replace(/\\([!"#$%&'()*+,\-./:;<=>?@[\]^_`{|}~\\])/g, '$1').replace(/&amp;/g, '&');

function visibleMarkdown(source) {
  let fence;
  return source
    .replace(/<!--[\s\S]*?-->/g, (comment) => comment.replace(/[^\n]/g, ' '))
    .split('\n')
    .map((line) => {
      const marker = line.match(/^ {0,3}(`{3,}|~{3,})/);
      if (fence) {
        if (marker && marker[1][0] === fence[0] && marker[1].length >= fence.length)
          fence = undefined;
        return '';
      }
      if (marker) {
        fence = marker[1];
        return '';
      }
      if (/^(?: {4}|\t)/.test(line)) return '';
      return line;
    })
    .join('\n');
}

function destination(source, start) {
  let index = start;
  while (/\s/.test(source[index] || '') && index < source.length) index++;
  if (source[index] === '<') {
    const end = source.indexOf('>', index + 1);
    return end === -1 ? undefined : source.slice(index + 1, end);
  }
  const from = index;
  let depth = 0;
  for (; index < source.length; index++) {
    const char = source[index];
    if (char === '\\') {
      index++;
      continue;
    }
    if (char === '(') depth++;
    else if (char === ')') {
      if (!depth) break;
      depth--;
    } else if (/\s/.test(char) && !depth) break;
  }
  return source.slice(from, index);
}

export function markdownReferences(source) {
  const visible = visibleMarkdown(source);
  const references = [];
  const definitions = new Map();
  const lineAt = (index) => visible.slice(0, index).split('\n').length;
  for (const match of visible.matchAll(/^ {0,3}\[([^\]\n]+)\]:\s*(.*)$/gm)) {
    const target = destination(match[2], 0);
    if (target === undefined) continue;
    definitions.set(normalize(match[1]), target);
    references.push({ target, line: lineAt(match.index), image: false });
  }
  // Inline code may demonstrate links without creating a rendered reference.
  const text = visible.replace(/(`+)([^`]*?)\1/g, (code) => code.replace(/[^\n]/g, ' '));
  for (let index = 0; index < text.length; index++) {
    if (text[index] !== '[' || (index > 0 && text[index - 1] === '\\')) continue;
    let end = index + 1,
      depth = 1;
    for (; end < text.length; end++) {
      if (text[end] === '\\') {
        end++;
        continue;
      }
      if (text[end] === '[') depth++;
      if (text[end] === ']' && --depth === 0) break;
    }
    if (end === text.length) continue;
    const label = text.slice(index + 1, end);
    let after = end + 1;
    while (/[ \t]/.test(text[after] || '') && after < text.length) after++;
    let target;
    if (text[after] === '(') target = destination(text, after + 1);
    else if (text[after] === '[') {
      const referenceEnd = text.indexOf(']', after + 1);
      if (referenceEnd !== -1)
        target = definitions.get(normalize(text.slice(after + 1, referenceEnd) || label));
    } else if (text[after] !== ':') target = definitions.get(normalize(label));
    if (target !== undefined)
      references.push({ target, line: lineAt(index), image: text[index - 1] === '!' });
  }
  for (const match of text.matchAll(/<(a|img)\b[^>]*?\b(?:href|src)\s*=\s*(["'])(.*?)\2[^>]*>/gi))
    references.push({
      target: match[3],
      line: lineAt(match.index),
      image: match[1].toLowerCase() === 'img',
    });
  return references.map((reference) => ({ ...reference, target: unescape(reference.target) }));
}

export function markdownAnchors(source) {
  const text = visibleMarkdown(source);
  const result = new Set();
  for (const match of text.matchAll(/<[^>]+\b(?:id|name)\s*=\s*(["'])(.*?)\1[^>]*>/gi))
    result.add(match[2]);
  const lines = text.split('\n');
  for (let index = 0; index < lines.length; index++) {
    const atx = lines[index].match(/^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$/);
    const setext =
      !atx && lines[index].trim() && /^ {0,3}(?:=+|-+)\s*$/.test(lines[index + 1] || '');
    if (!atx && !setext) continue;
    const label = (atx ? atx[1] : lines[index])
      .replace(/!?\[([^\]]+)\]\([^)]*\)/g, '$1')
      .replace(/<[^>]*>/g, '')
      .replace(/`/g, '');
    const slug = unescape(label)
      .trim()
      .toLowerCase()
      .replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, '')
      .replace(/\s/g, '-');
    let unique = slug,
      number = 0;
    while (result.has(unique)) unique = `${slug}-${++number}`;
    result.add(unique);
  }
  return result;
}

export function validateMarkdownFiles(root, files) {
  const errors = [];
  let checked = 0,
    images = 0;
  const anchors = new Map();
  for (const file of files) {
    const absolute = path.resolve(root, file);
    if (!fs.existsSync(absolute)) continue; // A tracked file may be deleted in this working tree.
    const source = fs.readFileSync(absolute, 'utf8');
    for (const reference of markdownReferences(source)) {
      const value = reference.target;
      if (/^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(value)) continue;
      checked++;
      if (reference.image) images++;
      const location = `${file}:${reference.line}`;
      let pathname, fragment;
      try {
        const [beforeFragment, ...rest] = value.split('#');
        pathname = decodeURIComponent(beforeFragment.split('?')[0]);
        fragment = rest.length ? decodeURIComponent(rest.join('#')) : '';
      } catch {
        errors.push(`${location}: malformed URL encoding: ${value}`);
        continue;
      }
      const target = pathname
        ? path.resolve(
            pathname.startsWith('/') ? root : path.dirname(absolute),
            pathname.replace(/^\//, ''),
          )
        : absolute;
      const relative = path.relative(root, target);
      if (relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
        errors.push(`${location}: local link leaves the repository: ${value}`);
        continue;
      }
      if (!fs.existsSync(target)) {
        errors.push(`${location}: missing local ${reference.image ? 'image' : 'target'}: ${value}`);
        continue;
      }
      if (reference.image && !fs.statSync(target).isFile()) {
        errors.push(`${location}: image target is not a file: ${value}`);
        continue;
      }
      if (fragment && /\.mdx?$/i.test(target)) {
        if (!anchors.has(target))
          anchors.set(target, markdownAnchors(fs.readFileSync(target, 'utf8')));
        if (!anchors.get(target).has(fragment))
          errors.push(`${location}: missing Markdown anchor: ${value}`);
      }
    }
  }
  return { errors, checked, images };
}

export function repositoryMarkdownFiles(root = ROOT) {
  return [
    ...new Set(
      execFileSync(
        'git',
        ['ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', '*.md', '*.mdx'],
        { cwd: root, encoding: 'utf8' },
      )
        .split('\0')
        .filter(Boolean),
    ),
  ].filter(
    (file) =>
      !file
        .split('/')
        .some((part) =>
          ['.agents', 'node_modules', '.cache', 'vendor', 'bin', 'obj', 'dist'].includes(part),
        ),
  );
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const result = validateMarkdownFiles(ROOT, repositoryMarkdownFiles());
  if (result.errors.length) {
    console.error(result.errors.join('\n'));
    process.exitCode = 1;
  } else
    console.log(
      `Markdown local links passed: ${result.checked} references, ${result.images} images`,
    );
}

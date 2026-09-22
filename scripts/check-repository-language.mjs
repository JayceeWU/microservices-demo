import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const HAN = /\p{Script=Han}/u;

function codePoint(value, radix) {
  const point = Number.parseInt(value, radix);
  return point <= 0x10ffff ? String.fromCodePoint(point) : '';
}

export function decodeText(source) {
  return source
    .replace(/\\u\{([\da-f]{1,6})\}|\\U([\da-f]{8})|\\u([\da-f]{4})/gi, (_, braced, long, short) =>
      codePoint(braced || long || short, 16),
    )
    .replace(/&#(?:x([\da-f]+)|(\d+));/gi, (_, hex, decimal) =>
      codePoint(hex || decimal, hex ? 16 : 10),
    )
    .replace(/(?:%[\da-f]{2})+/gi, (encoded) => {
      try {
        return decodeURIComponent(encoded);
      } catch {
        return encoded;
      }
    });
}

export function repositoryFiles(root = ROOT) {
  return [
    ...new Set(
      execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard'], {
        cwd: root,
        encoding: 'utf8',
        maxBuffer: 16 * 1024 * 1024,
      })
        .split('\0')
        .filter(Boolean),
    ),
  ];
}

export function validateRepositoryLanguage(root = ROOT) {
  const errors = [];
  let checked = 0;
  let binaries = 0;
  for (const file of repositoryFiles(root)) {
    const absolute = path.join(root, file);
    let stat;
    try {
      stat = fs.lstatSync(absolute);
    } catch (error) {
      if (error.code === 'ENOENT') continue;
      throw error;
    }
    if (HAN.test(decodeText(file))) errors.push(`${JSON.stringify(file)}: Han characters in path`);
    // Inspect symlink targets without following them outside the repository.
    if (!stat.isFile() && !stat.isSymbolicLink()) continue;
    const bytes = stat.isSymbolicLink()
      ? Buffer.from(fs.readlinkSync(absolute))
      : fs.readFileSync(absolute);
    const utf16 =
      bytes[0] === 0xff && bytes[1] === 0xfe
        ? 'utf-16le'
        : bytes[0] === 0xfe && bytes[1] === 0xff
          ? 'utf-16be'
          : undefined;
    if (!utf16 && bytes.includes(0)) {
      binaries++;
      continue;
    }
    let source;
    try {
      source = new TextDecoder(utf16 || 'utf-8', { fatal: true }).decode(bytes);
    } catch {
      errors.push(`${file}: text must use valid UTF-8 or BOM-marked UTF-16`);
      continue;
    }
    checked++;
    source.split(/\r?\n/).forEach((line, index) => {
      const match = decodeText(line).match(HAN);
      if (match)
        errors.push(
          `${file}:${index + 1}: Han character U+${match[0].codePointAt(0).toString(16).toUpperCase()}`,
        );
    });
  }
  return { errors, checked, binaries };
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const result = validateRepositoryLanguage();
  if (result.errors.length) {
    console.error(result.errors.join('\n'));
    process.exitCode = 1;
  } else {
    console.log(
      `Language check passed: ${result.checked} text files and all repository paths contain no literal or encoded Han characters (${result.binaries} binary files skipped).`,
    );
  }
}

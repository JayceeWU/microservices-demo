import fs from 'node:fs/promises';
import path from 'node:path';
import { createHash, randomUUID } from 'node:crypto';

export const versions = Object.freeze({
  node: '22.16.0',
  go: '1.25.0',
  kind: '0.30.0',
  kubernetes: '1.34.0',
  calico: '3.32.0',
  skaffold: '2.24.0',
  helm: '3.18.6',
  kubectl: '1.34.0',
  buf: '1.57.2',
  kubeconform: '0.6.7',
  actionlint: '1.7.7',
});

export const nodeImage =
  'kindest/node:v1.34.0@sha256:7416a61b42b1662ca6ca89f02028ac133a309a2a30ba309614e8ec94d976dc5a';
export const calicoDigest = 'bccabc607685551db918f66da724893eca3e69a50c5a3e3077029b02dbab8d35';
export const digest = (buffer) => createHash('sha256').update(buffer).digest('hex');

// Official release digests: nodejs.org/en/blog/release/v22.16.0 and go.dev/dl/.
export function toolchainArtifact(name, platform = process.platform, arch = process.arch) {
  if (!['win32', 'linux'].includes(platform) || arch !== 'x64')
    throw new Error('Acceptance toolchains support Windows/Linux x64.');
  const windows = platform === 'win32';
  if (name === 'node') {
    const folder = `node-v${versions.node}-${windows ? 'win' : 'linux'}-x64`;
    return {
      name,
      version: versions.node,
      folder,
      url: `https://nodejs.org/dist/v${versions.node}/${folder}.${windows ? 'zip' : 'tar.gz'}`,
      sha256: windows
        ? '21c2d9735c80b8f86dab19305aa6a9f6f59bbc808f68de3eef09d5832e3bfbbd'
        : 'fb870226119d47378fa9c92c4535389c72dae14fcc7b47e6fdcc82c43de5a547',
      executable: windows ? 'node.exe' : 'bin/node',
      npm: windows ? 'node_modules/npm/bin/npm-cli.js' : 'lib/node_modules/npm/bin/npm-cli.js',
    };
  }
  if (name === 'go')
    return {
      name,
      version: versions.go,
      folder: 'go',
      url: `https://go.dev/dl/go${versions.go}.${windows ? 'windows' : 'linux'}-amd64.${windows ? 'zip' : 'tar.gz'}`,
      sha256: windows
        ? '89efb4f9b30812eee083cc1770fdd2913c14d301064f6454851428f9707d190b'
        : '2852af0cb20a13139b3448992e69b868e50ed0f8a1e5940ee1de9e19a123b613',
      executable: windows ? 'bin/go.exe' : 'bin/go',
    };
  throw new Error(`Unknown acceptance toolchain: ${name}`);
}

export async function installConcurrently(items, concurrency, operation) {
  const queue = [...items];
  const workers = Array.from({ length: Math.min(concurrency, queue.length) }, async () => {
    while (queue.length) await operation(queue.shift());
  });
  // Wait for every active installer before propagating failures, so report and
  // cleanup never race another installer's child process or file writes.
  const results = await Promise.allSettled(workers);
  const failures = results
    .filter((result) => result.status === 'rejected')
    .map((result) => result.reason);
  if (failures.length) throw new AggregateError(failures, 'Acceptance tool installation failed');
}

export async function ensureToolchains(cache, run) {
  const result = {};
  await installConcurrently(['node', 'go'], 2, async (name) => {
    const item = toolchainArtifact(name);
    const directory = path.join(cache, `${name}-${item.version}-${process.platform}`);
    const root = path.join(directory, item.folder);
    const executable = path.join(root, item.executable);
    const receipt = path.join(directory, 'verified.json');
    await fs.mkdir(directory, { recursive: true });
    const integrityFiles = [
      executable,
      path.join(root, 'LICENSE'),
      ...(item.npm ? [path.join(root, item.npm)] : []),
    ];
    const integrity = async () =>
      Object.fromEntries(
        await Promise.all(
          integrityFiles.map(async (file) => [
            path.relative(directory, file),
            digest(await fs.readFile(file)),
          ]),
        ),
      );
    let verified = false;
    try {
      const saved = JSON.parse(await fs.readFile(receipt, 'utf8'));
      verified =
        saved.url === item.url &&
        saved.archiveSha256 === item.sha256 &&
        JSON.stringify(saved.files) === JSON.stringify(await integrity());
    } catch {
      /* A fresh or incomplete toolchain must be verified again. */
    }
    if (!verified) {
      console.log(`Installing ${name} ${item.version} with its official release SHA256`);
      const archive = path.join(directory, new URL(item.url).pathname.split('/').at(-1));
      let bytes;
      try {
        bytes = await fs.readFile(archive);
      } catch {
        /* New cache. */
      }
      if (!bytes || digest(bytes) !== item.sha256)
        bytes = await fetchBytes(item.url, { run, directory });
      if (digest(bytes) !== item.sha256) throw new Error(`Checksum mismatch for ${name} toolchain`);
      await fs.writeFile(archive, bytes);
      // The extracted SDK is needed at runtime; only its verified archive is disposable.
      await run('tar', ['-xf', archive, '-C', directory], { label: `install-${name}` });
      await fs.writeFile(
        receipt,
        JSON.stringify(
          { url: item.url, archiveSha256: item.sha256, files: await integrity() },
          null,
          2,
        ),
      );
      await fs.rm(archive);
    }
    result[name] = executable;
    if (item.npm) result.npm = path.join(root, item.npm);
    if (name === 'go') result.goroot = root;
  });
  return result;
}

export function artifact(name, platform = process.platform, arch = process.arch) {
  if (!['win32', 'linux'].includes(platform) || arch !== 'x64') {
    throw new Error(
      'Acceptance tools currently support Windows/Linux x64, matching the project linux/amd64 images.',
    );
  }
  const os = platform === 'win32' ? 'windows' : 'linux';
  const extension = platform === 'win32' ? '.exe' : '';
  const archive = platform === 'win32' ? 'zip' : 'tar.gz';
  const version = versions[name];
  let url,
    checksum,
    executable = name + extension,
    member;
  if (name === 'kind') {
    url = `https://github.com/kubernetes-sigs/kind/releases/download/v${version}/kind-${os}-amd64`;
    checksum = `${url}.sha256sum`;
  } else if (name === 'kubectl') {
    url = `https://dl.k8s.io/release/v${version}/bin/${os}/amd64/kubectl${extension}`;
    checksum = `${url}.sha256`;
  } else if (name === 'skaffold') {
    url = `https://github.com/GoogleContainerTools/skaffold/releases/download/v${version}/skaffold-${os}-amd64${extension}`;
    checksum = `${url}.sha256`;
  } else if (name === 'helm') {
    url = `https://get.helm.sh/helm-v${version}-${os}-amd64.${archive}`;
    checksum = `${url}.sha256sum`;
    member = `${os}-amd64/${executable}`;
  } else if (name === 'buf') {
    url = `https://github.com/bufbuild/buf/releases/download/v${version}/buf-${platform === 'win32' ? 'Windows' : 'Linux'}-x86_64${extension}`;
    checksum = `https://github.com/bufbuild/buf/releases/download/v${version}/sha256.txt`;
  } else if (name === 'kubeconform') {
    url = `https://github.com/yannh/kubeconform/releases/download/v${version}/kubeconform-${os}-amd64.${archive}`;
    checksum = `https://github.com/yannh/kubeconform/releases/download/v${version}/CHECKSUMS`;
    member = executable;
  } else if (name === 'actionlint') {
    url = `https://github.com/rhysd/actionlint/releases/download/v${version}/actionlint_${version}_${os}_amd64.${archive}`;
    checksum = `https://github.com/rhysd/actionlint/releases/download/v${version}/actionlint_${version}_checksums.txt`;
    member = executable;
  } else {
    throw new Error(`Unknown acceptance tool: ${name}`);
  }
  return { name, version, url, checksum, executable, member };
}

export function checksumFor(text, filename) {
  const lines = text.trim().split(/\r?\n/);
  const matching = lines.find(
    (line) => line.trim().split(/\s+/).at(-1).replace(/^\*/, '') === filename,
  );
  const value = (matching || (lines.length === 1 ? lines[0] : '')).trim().split(/\s+/)[0];
  if (!/^[a-f0-9]{64}$/i.test(value)) throw new Error(`No SHA256 for ${filename}`);
  return value.toLowerCase();
}

export async function fetchBytes(
  url,
  {
    run,
    directory,
    fetchImpl = fetch,
    log = console.log,
    platform = process.platform,
    attempts = 3,
    idleTimeout = 45_000,
  } = {},
) {
  let last;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    log(`Download attempt ${attempt}/${attempts}: ${url}`);
    try {
      if (platform === 'win32' && run && directory) {
        const partial = path.join(directory, `download-${randomUUID()}.part`);
        const started = Date.now();
        const progress = setInterval(async () => {
          try {
            const { size } = await fs.stat(partial);
            log(
              `Download ${new URL(url).hostname}: ${(size / 1024 / 1024).toFixed(2)} MiB, ${Math.round((Date.now() - started) / 1000)}s (curl)`,
            );
          } catch {
            /* The transfer may still be connecting or have just completed. */
          }
        }, 10_000);
        try {
          await run(
            'curl.exe',
            [
              '--fail',
              '--location',
              '--connect-timeout',
              '30',
              '--max-time',
              '1200',
              '--speed-limit',
              '1024',
              '--speed-time',
              '60',
              '--silent',
              '--show-error',
              '--output',
              partial,
              '--url',
              url,
            ],
            { label: 'download-curl', timeout: 21 * 60_000 },
          );
          const bytes = await fs.readFile(partial);
          log(`Downloaded ${(bytes.length / 1024 / 1024).toFixed(2)} MiB with curl`);
          return bytes;
        } catch (error) {
          log(`curl download failed: ${error.message}; trying streaming Node transport`);
        } finally {
          clearInterval(progress);
          await fs.rm(partial, { force: true });
        }
      }
      const controller = new AbortController();
      let received = 0,
        total = 0;
      const started = Date.now();
      const progress = () =>
        log(
          `Download ${new URL(url).hostname}: ${(received / 1024 / 1024).toFixed(2)}${total ? `/${(total / 1024 / 1024).toFixed(2)}` : ''} MiB, ${Math.round((Date.now() - started) / 1000)}s`,
        );
      let idle;
      const resetIdle = () => {
        clearTimeout(idle);
        idle = setTimeout(
          () => controller.abort(new Error(`Download received no data for ${idleTimeout / 1000}s`)),
          idleTimeout,
        );
      };
      const overall = setTimeout(
        () => controller.abort(new Error('Download exceeded 20 minutes')),
        20 * 60_000,
      );
      const ticker = setInterval(progress, 10_000);
      resetIdle();
      try {
        const response = await fetchImpl(url, { signal: controller.signal });
        if (!response.ok) throw new Error(`Download HTTP ${response.status}: ${url}`);
        total = Number(response.headers.get('content-length')) || 0;
        const chunks = [];
        for await (const chunk of response.body) {
          chunks.push(chunk);
          received += chunk.length;
          resetIdle();
        }
        progress();
        return Buffer.concat(chunks);
      } finally {
        clearTimeout(idle);
        clearTimeout(overall);
        clearInterval(ticker);
      }
    } catch (error) {
      last = error;
      log(
        `Download attempt ${attempt} failed: ${error.name}: ${error.message}${error.cause?.code ? ` (${error.cause.code})` : ''}`,
      );
    }
  }
  throw last;
}

export async function ensureTools(names, cache, run) {
  const tools = {};
  await installConcurrently([...new Set(names)], 3, async (name) => {
    const item = artifact(name);
    const directory = path.join(cache, `${name}-${item.version}-${process.platform}`);
    const executable = path.join(directory, item.executable);
    const receipt = path.join(directory, 'verified.json');
    await fs.mkdir(directory, { recursive: true });
    let verified = false;
    try {
      const saved = JSON.parse(await fs.readFile(receipt, 'utf8'));
      verified =
        saved.url === item.url &&
        saved.binarySha256 === digest(await fs.readFile(executable)) &&
        saved.licenseSha256 === digest(await fs.readFile(path.join(directory, 'LICENSE')));
    } catch {
      /* A fresh or incomplete cache must be verified from the release checksum. */
    }
    if (!verified) {
      console.log(`Installing ${name} ${item.version} from the official release`);
      const filename = new URL(item.url).pathname.split('/').at(-1);
      const expected = checksumFor(
        (await fetchBytes(item.checksum, { run, directory })).toString('utf8'),
        filename,
      );
      const downloaded = path.join(directory, filename);
      let bytes;
      try {
        bytes = await fs.readFile(downloaded);
      } catch {
        /* A missing or incomplete download is fetched below. */
      }
      if (!bytes || digest(bytes) !== expected)
        bytes = await fetchBytes(item.url, { run, directory });
      if (digest(bytes) !== expected) throw new Error(`Checksum mismatch for ${name}`);
      await fs.writeFile(downloaded, bytes);
      if (item.member) {
        await run('tar', ['-xf', downloaded, '-C', directory]);
        const extracted = path.join(directory, item.member);
        if (extracted !== executable) await fs.copyFile(extracted, executable);
        const extractedDirectory = path.dirname(extracted);
        if (extractedDirectory !== directory) {
          for (const license of ['LICENSE', 'LICENSE.txt']) {
            try {
              await fs.copyFile(
                path.join(extractedDirectory, license),
                path.join(directory, 'LICENSE'),
              );
            } catch {
              /* Some binary archives omit a license. */
            }
          }
          if (path.dirname(path.resolve(extractedDirectory)) !== path.resolve(directory))
            throw new Error('Unexpected extracted tool directory');
          await fs.rm(extractedDirectory, { recursive: true, force: true });
        }
      } else if (downloaded !== executable) {
        await fs.copyFile(downloaded, executable);
      }
      if (downloaded !== executable) await fs.rm(downloaded, { force: true });
      const repositories = {
        kind: 'kubernetes-sigs/kind',
        kubectl: 'kubernetes/kubernetes',
        skaffold: 'GoogleContainerTools/skaffold',
        helm: 'helm/helm',
        buf: 'bufbuild/buf',
        kubeconform: 'yannh/kubeconform',
        actionlint: 'rhysd/actionlint',
      };
      const license = path.join(directory, 'LICENSE');
      try {
        await fs.access(license);
      } catch {
        try {
          await fs.copyFile(path.join(directory, 'LICENSE.txt'), license);
        } catch {
          const licenseName = name === 'actionlint' ? 'LICENSE.txt' : 'LICENSE';
          await fs.writeFile(
            license,
            await fetchBytes(
              `https://raw.githubusercontent.com/${repositories[name]}/v${item.version}/${licenseName}`,
              { run, directory },
            ),
          );
        }
      }
      if (process.platform !== 'win32') await fs.chmod(executable, 0o755);
      await fs.writeFile(
        receipt,
        JSON.stringify(
          {
            url: item.url,
            archiveSha256: expected,
            binarySha256: digest(await fs.readFile(executable)),
            licenseSha256: digest(await fs.readFile(license)),
          },
          null,
          2,
        ),
      );
      const retained = new Set([item.executable, 'LICENSE', 'verified.json']);
      for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
        if (entry.isFile() && !retained.has(entry.name))
          await fs.rm(path.join(directory, entry.name));
      }
    }
    tools[name] = executable;
  });
  return tools;
}

export async function ensureCalico(cache, run) {
  const file = path.join(cache, `calico-${versions.calico}.yaml`);
  let bytes;
  try {
    bytes = await fs.readFile(file);
  } catch {
    /* New cache. */
  }
  if (!bytes || digest(bytes) !== calicoDigest) {
    await fs.mkdir(cache, { recursive: true });
    bytes = await fetchBytes(
      `https://raw.githubusercontent.com/projectcalico/calico/v${versions.calico}/manifests/calico.yaml`,
      { run, directory: cache },
    );
    if (digest(bytes) !== calicoDigest)
      throw new Error('Calico manifest SHA256 differs from the accepted manifest');
    await fs.mkdir(cache, { recursive: true });
    await fs.writeFile(file, bytes);
  }
  return file;
}

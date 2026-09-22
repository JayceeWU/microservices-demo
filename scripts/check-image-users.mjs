import { execFileSync } from 'node:child_process';
import process from 'node:process';

const EXPECTED_COMPOSE_USERS = new Map([
  ['migrations', '65532:65532'],
  ['accountservice', '65532:65532'],
  ['catalogservice', '65532:65532'],
  ['schedulingservice', '65532:65532'],
  ['orderservice', '65532:65532'],
  ['payrollservice', '65532:65532'],
  ['gatewayservice', '65532:65532'],
  ['chatservice', '65532:65532'],
  ['chat-media-worker', '65532:65532'],
  ['paymentservice', '65532:65532'],
  ['recommendationservice', '65532:65532'],
  ['outbox-relay', '65532:65532'],
  ['payment-order-saga', '65532:65532'],
  ['creditservice', '1654:1654'],
  ['cartservice', '1000:1000'],
  ['student-web', '1000:1000'],
  ['teacher-web', '101:101'],
  ['admin-web', '101:101'],
]);

function command(...args) {
  return execFileSync('docker', args, { encoding: 'utf8' }).trim();
}

function inspect(image, expected, label) {
  const configured = command('image', 'inspect', '--format', '{{.Config.User}}', image);
  if (!/^\d+:\d+$/.test(configured) || configured.startsWith('0:') || configured.endsWith(':0')) {
    throw new Error(
      `${label}: Config.User must be a non-zero numeric UID:GID, got ${JSON.stringify(configured)}`,
    );
  }
  if (configured !== expected) {
    throw new Error(`${label}: expected Config.User ${expected}, got ${configured}`);
  }
  console.log(`${label}: ${configured}`);
}

const compose = JSON.parse(command('compose', '--profile', 'full', 'config', '--format', 'json'));
for (const [service, expected] of EXPECTED_COMPOSE_USERS) {
  const image = compose.services?.[service]?.image ?? `${compose.name}-${service}`;
  inspect(image, expected, service);
}

for (const argument of process.argv.slice(2)) {
  const separator = argument.lastIndexOf('=');
  if (separator < 1) throw new Error(`invalid image assertion ${argument}; expected IMAGE=UID:GID`);
  inspect(
    argument.slice(0, separator),
    argument.slice(separator + 1),
    argument.slice(0, separator),
  );
}
